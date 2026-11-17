package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"

	"codefree2api/internal/srd"
)

const defaultSpaceVersion = "1.5.1"

// Space speaks the CodeFree Space completions API.
type Space struct {
	remote
	Version string
	Dir     string

	projectOnce sync.Once
	projectID   string
	verOnce     sync.Once
	readyVer    string
}

func (s *Space) NativeTools() bool { return true }

func (s *Space) Models(ctx context.Context) ([]ModelInfo, error) {
	return s.models(ctx, true)
}

func (s *Space) models(ctx context.Context, retry bool) ([]ModelInfo, error) {
	sess, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	status, body, err := s.caller(sess).Do(ctx, srd.Call{
		Method:    http.MethodGet,
		Path:      modelsPath,
		ProjectID: s.projectHeader(ctx, sess),
		GitURLs:   srd.ListRemotes(ctx, s.dir()),
	})
	if err != nil {
		return nil, errUpstream
	}
	if status == http.StatusUnauthorized && retry {
		s.clear()
		return s.models(ctx, false)
	}
	if status != http.StatusOK {
		return nil, errUpstream
	}
	ids := srd.ModelIDs(body)
	if len(ids) == 0 {
		return nil, errNoModels
	}
	out := make([]ModelInfo, 0, len(ids))
	for _, id := range ids {
		out = append(out, ModelInfo{ID: id})
	}
	return out, nil
}

func (s *Space) Run(ctx context.Context, req RunRequest, emit func(Delta) error) (RunResult, error) {
	res, err := s.run(ctx, req, emit, true)
	if err != nil {
		return RunResult{}, err
	}
	if len(res.Tools) == 0 && req.ClientTools {
		if text, calls, ok := parseToolEnvelope(nil, res.Text); ok {
			res.Text = text
			res.Tools = calls
			if len(calls) > 0 {
				res.Finish = "tool_calls"
			}
		}
	}
	return res, nil
}

func (s *Space) run(ctx context.Context, req RunRequest, emit func(Delta) error, retry bool) (RunResult, error) {
	sess, err := s.session(ctx)
	if err != nil {
		return RunResult{}, err
	}
	body, err := spaceBody(req)
	if err != nil {
		return RunResult{}, errUpstream
	}
	header := make(http.Header)
	header.Set("Content-Type", "application/json")
	header.Set("subService", "codefree-ide-agent")
	header.Set("modelName", req.ModelID)
	header.Set("X-Codefree-Quota-Schema", "1")
	if req.SessionID != "" {
		header.Set("sessionId", req.SessionID)
	}
	if id, err := srd.Nonce(s.now()); err == nil {
		header.Set("reqId", id)
	}
	resp, err := s.caller(sess).Open(ctx, srd.Call{
		Method:    http.MethodPost,
		Path:      completionsPath,
		Body:      body,
		Header:    header,
		ProjectID: s.projectHeader(ctx, sess),
		GitURLs:   srd.ListRemotes(ctx, s.dir()),
	})
	if err != nil {
		return RunResult{}, errUpstream
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized && retry {
		s.clear()
		return s.run(ctx, req, emit, false)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return RunResult{}, errQuota
	}
	if resp.StatusCode != http.StatusOK {
		return RunResult{}, errUpstream
	}
	return readSpaceSSE(resp.Body, emit)
}

func spaceBody(req RunRequest) ([]byte, error) {
	var messages []map[string]string
	if strings.TrimSpace(req.System) != "" {
		messages = append(messages, map[string]string{"role": "system", "content": req.System})
	}
	messages = append(messages, map[string]string{"role": "user", "content": promptText(req)})
	body := map[string]any{
		"messages": messages,
		"model":    req.ModelID,
		"stream":   true,
	}
	if tools := openAITools(req.Tools, req.ToolChoice); tools != nil {
		body["tools"] = tools
		if choice := openAIToolChoice(req.ToolChoice); choice != nil {
			body["tool_choice"] = choice
		}
	}
	return srd.Marshal(body)
}

func openAITools(tools []ToolDef, choice ToolChoice) []any {
	if len(tools) == 0 || choice.Mode == "none" {
		return nil
	}
	var out []any
	for _, tool := range tools {
		params := tool.Parameters
		if len(bytesTrimSpace(params)) == 0 {
			params = json.RawMessage("{}")
		}
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  params,
			},
		})
	}
	return out
}

func openAIToolChoice(choice ToolChoice) any {
	switch choice.Mode {
	case "", "auto":
		return nil
	case "none":
		return "none"
	case "required":
		return "required"
	case "tool":
		return map[string]any{"type": "function", "function": map[string]any{"name": choice.Name}}
	default:
		return nil
	}
}

func readSpaceSSE(r io.Reader, emit func(Delta) error) (RunResult, error) {
	sc := bufio.NewReader(r)
	var text strings.Builder
	var finish string
	var usage Usage
	tools := map[int]*ToolCall{}
	var order []int
	for {
		line, err := sc.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimRight(line, "\r\n")
			done, chunk, bad := spaceEvent(line)
			if bad != nil {
				return RunResult{}, errUpstream
			}
			if done {
				break
			}
			if chunk != nil {
				if err := applySpaceChunk(chunk, &text, &finish, &usage, tools, &order, emit); err != nil {
					return RunResult{}, err
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return RunResult{}, errUpstream
		}
	}
	res := RunResult{Text: text.String(), Usage: usage, Finish: finish, Tools: orderedTools(tools, order)}
	if res.Finish == "" {
		if len(res.Tools) > 0 {
			res.Finish = "tool_calls"
		} else {
			res.Finish = "stop"
		}
	}
	return res, nil
}

func spaceEvent(line string) (bool, map[string]any, error) {
	switch {
	case line == "" || strings.HasPrefix(line, "event:"):
		return false, nil, nil
	case strings.HasPrefix(line, ": ping") || line == "data: [DONE]" || line == "data:[DONE]":
		return true, nil, nil
	case strings.HasPrefix(line, ":"):
		return false, nil, nil
	case strings.HasPrefix(line, "data:"):
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if raw == "[DONE]" {
			return true, nil, nil
		}
		dec := json.NewDecoder(strings.NewReader(raw))
		dec.UseNumber()
		var v map[string]any
		if err := dec.Decode(&v); err != nil {
			return false, nil, err
		}
		return false, v, nil
	default:
		return false, nil, nil
	}
}

func applySpaceChunk(chunk map[string]any, text *strings.Builder, finish *string, usage *Usage, tools map[int]*ToolCall, order *[]int, emit func(Delta) error) error {
	if u, ok := chunk["usage"].(map[string]any); ok {
		usage.Input = jsonIntValue(u["prompt_tokens"])
		usage.Output = jsonIntValue(u["completion_tokens"])
	}
	choices, _ := chunk["choices"].([]any)
	if len(choices) == 0 {
		return nil
	}
	choice, _ := choices[0].(map[string]any)
	if choice == nil {
		return nil
	}
	if reason := scalarString(choice["finish_reason"]); reason != "" {
		*finish = mapFinish(reason)
	}
	delta, _ := choice["delta"].(map[string]any)
	if delta == nil {
		delta, _ = choice["message"].(map[string]any)
	}
	if delta == nil {
		return nil
	}
	if content := scalarString(delta["content"]); content != "" {
		text.WriteString(content)
		if emit != nil {
			if err := emit(Delta{Text: content}); err != nil {
				return err
			}
		}
	}
	calls, _ := delta["tool_calls"].([]any)
	for i, item := range calls {
		call, _ := item.(map[string]any)
		if call == nil {
			continue
		}
		idx := i
		if n, ok := jsonIndex(call["index"]); ok {
			idx = n
		}
		acc := tools[idx]
		if acc == nil {
			acc = &ToolCall{}
			tools[idx] = acc
			*order = append(*order, idx)
		}
		if id := scalarString(call["id"]); id != "" {
			acc.ID = id
		}
		fn, _ := call["function"].(map[string]any)
		if name := scalarString(fn["name"]); name != "" {
			acc.Name = name
		}
		if args := scalarString(fn["arguments"]); args != "" {
			acc.Arguments += args
		}
	}
	return nil
}

func orderedTools(tools map[int]*ToolCall, order []int) []ToolCall {
	var out []ToolCall
	for _, idx := range order {
		call := tools[idx]
		if call == nil || strings.TrimSpace(call.Name) == "" {
			continue
		}
		if call.ID == "" {
			call.ID = newID("call")
		}
		if call.Arguments == "" {
			call.Arguments = "{}"
		}
		out = append(out, *call)
	}
	return out
}

func jsonIntValue(v any) int {
	n, ok := jsonIndex(v)
	if !ok {
		return 0
	}
	return n
}

func jsonIndex(v any) (int, bool) {
	switch t := v.(type) {
	case json.Number:
		n, err := t.Int64()
		return int(n), err == nil
	default:
		return 0, false
	}
}

func scalarString(v any) string {
	s, _ := v.(string)
	return s
}

func (s *Space) projectHeader(ctx context.Context, sess *srd.Session) string {
	s.lookupProject(ctx, sess)
	if s.projectID == "" {
		return "0"
	}
	return s.projectID
}

func (s *Space) lookupProject(ctx context.Context, sess *srd.Session) {
	s.projectOnce.Do(func() {
		urls := srd.ListRemotes(ctx, s.dir())
		if len(urls) == 0 {
			s.projectID = "0"
			return
		}
		payload, err := srd.Marshal(map[string]any{
			"gitUrls":    urls,
			"clientType": "codefree-ide",
			"version":    s.clientVersion(ctx, sess),
		})
		if err != nil {
			s.projectID = "0"
			return
		}
		status, body, err := s.caller(sess).Do(ctx, srd.Call{
			Method:    http.MethodPost,
			Path:      projectPath,
			Body:      payload,
			ProjectID: "0",
			GitURLs:   urls,
		})
		if err != nil || status != http.StatusOK {
			s.projectID = "0"
			return
		}
		id, _ := srd.Project(body)
		s.projectID = strconv.FormatInt(id, 10)
	})
}

func (s *Space) clientVersion(ctx context.Context, sess *srd.Session) string {
	if strings.TrimSpace(s.Version) != "" {
		return strings.TrimSpace(s.Version)
	}
	s.verOnce.Do(func() {
		s.readyVer = defaultSpaceVersion
		status, body, err := s.caller(sess).Do(ctx, srd.Call{
			Method:    http.MethodGet,
			Path:      releasesPath,
			Query:     url.Values{"onlyLast": {"true"}},
			ProjectID: "0",
		})
		if err == nil && status == http.StatusOK {
			if found := srd.SpaceVersion(body); found != "" {
				s.readyVer = found
			}
		}
	})
	return s.readyVer
}

func (s *Space) dir() string {
	if s.Dir != "" {
		return s.Dir
	}
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}

func promptText(req RunRequest) string {
	if len(req.Files) == 0 {
		return req.Prompt
	}
	var b strings.Builder
	b.WriteString(req.Prompt)
	for _, file := range req.Files {
		if file.Name == "" {
			continue
		}
		b.WriteString("\n")
		b.WriteString(file.Name)
	}
	return b.String()
}
