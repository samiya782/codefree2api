package proxy

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Codefree drives a local `codefree-o serve` process. The child keeps the
// existing login environment. This process does not read credential files
// and does not set SRDCLOUD_* or an upstream API key.
type Codefree struct {
	Bin string
	Dir string

	startMu sync.Mutex
	mu      sync.Mutex
	cmd     *exec.Cmd
	baseURL string
	pass    string
	client  *http.Client
	started bool
	// external is set in tests that point baseURL at an httptest server.
	external bool
}

type upstreamError struct {
	Status int
	Body   string
}

func (e *upstreamError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("codefree-o HTTP %d", e.Status)
	}
	return fmt.Sprintf("codefree-o HTTP %d: %s", e.Status, e.Body)
}

var listenRE = regexp.MustCompile(`listening on (https?://\S+)`)

func (c *Codefree) Models(ctx context.Context) ([]ModelInfo, error) {
	if err := c.ensure(ctx); err == nil {
		models, err := c.providerModels(ctx)
		if err == nil && len(models) > 0 {
			return models, nil
		}
	}
	return c.cliModels(ctx)
}

func (c *Codefree) cliModels(ctx context.Context) ([]ModelInfo, error) {
	r := &Runner{Bin: c.bin(), WorkDir: c.Dir}
	ids, err := r.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ModelInfo, 0, len(ids))
	for _, id := range ids {
		out = append(out, ModelInfo{ID: id})
	}
	return out, nil
}

func (c *Codefree) providerModels(ctx context.Context) ([]ModelInfo, error) {
	var body struct {
		All []struct {
			ID     string `json:"id"`
			Models map[string]struct {
				ID string `json:"id"`
			} `json:"models"`
		} `json:"all"`
	}
	if err := c.do(ctx, http.MethodGet, "/provider", nil, nil, &body); err != nil {
		return nil, err
	}
	var out []ModelInfo
	seen := map[string]bool{}
	for _, p := range body.All {
		if p.ID != "" && p.ID != "codefree" {
			continue
		}
		prefix := p.ID
		if prefix == "" {
			prefix = "codefree"
		}
		for id := range p.Models {
			full := prefix + "/" + id
			if seen[full] {
				continue
			}
			seen[full] = true
			out = append(out, ModelInfo{ID: full})
		}
	}
	sortModels(out)
	return out, nil
}

func sortModels(models []ModelInfo) {
	for i := 1; i < len(models); i++ {
		j := i
		for j > 0 && models[j].ID < models[j-1].ID {
			models[j], models[j-1] = models[j-1], models[j]
			j--
		}
	}
}

func (c *Codefree) Run(ctx context.Context, req RunRequest, emit func(Delta) error) (RunResult, error) {
	if err := c.ensure(ctx); err != nil {
		return RunResult{}, err
	}
	var emitted bool
	wrapped := emit
	if emit != nil {
		wrapped = func(d Delta) error {
			emitted = true
			return emit(d)
		}
	}
	res, err := c.dispatch(ctx, req, wrapped)
	if err != nil && !emitted && req.Variant != "" && variantRejected(err) {
		req.Variant = ""
		return c.dispatch(ctx, req, wrapped)
	}
	return res, err
}

func variantRejected(err error) bool {
	var ue *upstreamError
	if !errors.As(err, &ue) {
		return false
	}
	if ue.Status != http.StatusBadRequest {
		return false
	}
	return strings.Contains(strings.ToLower(ue.Body), "variant")
}

func (c *Codefree) dispatch(ctx context.Context, req RunRequest, emit func(Delta) error) (RunResult, error) {
	if req.ClientTools || emit == nil {
		return c.runSync(ctx, req)
	}
	return c.runStream(ctx, req, emit)
}

func (c *Codefree) runSync(ctx context.Context, req RunRequest) (RunResult, error) {
	sid, err := c.sessionID(ctx, req)
	if err != nil {
		return RunResult{}, err
	}
	var resp assistantPayload
	if err := c.do(ctx, http.MethodPost, "/session/"+url.PathEscape(sid)+"/message", c.dirQuery(), promptBody(req), &resp); err != nil {
		return RunResult{}, err
	}
	res := resp.toResult(sid, req.ClientTools)
	return res, res.fail()
}

func (c *Codefree) runStream(ctx context.Context, req RunRequest, emit func(Delta) error) (RunResult, error) {
	evCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	events, err := c.openEvents(evCtx)
	if err != nil {
		return RunResult{}, err
	}
	defer events.Close()

	sid, err := c.sessionID(ctx, req)
	if err != nil {
		return RunResult{}, err
	}
	if err := c.do(ctx, http.MethodPost, "/session/"+url.PathEscape(sid)+"/prompt_async", c.dirQuery(), promptBody(req), nil); err != nil {
		return RunResult{}, err
	}

	acc := newAccumulator(sid)
	var streamed strings.Builder
	readErr := c.consume(ctx, events, acc, func(d Delta) error {
		if d.Text != "" {
			streamed.WriteString(d.Text)
		}
		return emit(d)
	})
	final, ferr := c.fetchAssistant(ctx, sid)
	if ferr == nil {
		acc.applyPayload(final, req.ClientTools)
	}
	res := acc.result()
	if emit != nil {
		if rest := suffix(res.Text, streamed.String()); rest != "" {
			if err := emit(Delta{Text: rest}); err != nil && readErr == nil {
				readErr = err
			}
		}
	}
	if err := res.fail(); err != nil {
		return res, err
	}
	if readErr != nil && !errors.Is(readErr, io.EOF) && res.Text == "" && len(res.Tools) == 0 && !acc.completed {
		return res, readErr
	}
	return res, nil
}

func suffix(full, sent string) string {
	if sent == "" {
		return full
	}
	if strings.HasPrefix(full, sent) {
		return full[len(sent):]
	}
	return ""
}

func (c *Codefree) consume(ctx context.Context, body io.Reader, acc *accumulator, emit func(Delta) error) error {
	ch := make(chan cfEvent, 32)
	errCh := make(chan error, 1)
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		defer close(ch)
		r := bufio.NewReader(body)
		for {
			data, err := readSSE(r)
			if data != "" {
				var ev cfEvent
				if json.Unmarshal([]byte(data), &ev) == nil && ev.Type != "" {
					select {
					case ch <- ev:
					case <-readCtx.Done():
						return
					}
				}
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					select {
					case errCh <- err:
					default:
					}
				}
				return
			}
		}
	}()
	var sawBusy bool
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-ch:
			if !ok {
				select {
				case err := <-errCh:
					return err
				default:
					return nil
				}
			}
			done, err := acc.event(ev, &sawBusy, emit)
			if err != nil {
				return err
			}
			if done {
				return nil
			}
		}
	}
}

func (c *Codefree) sessionID(ctx context.Context, req RunRequest) (string, error) {
	if req.SessionID != "" {
		return req.SessionID, nil
	}
	body := map[string]any{"title": "codefree2api"}
	if req.ClientTools {
		body["permission"] = []map[string]string{{
			"permission": "*",
			"pattern":    "*",
			"action":     "deny",
		}}
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/session", c.dirQuery(), body, &created); err != nil {
		return "", err
	}
	if created.ID == "" {
		return "", fmt.Errorf("codefree-o did not return a session id")
	}
	return created.ID, nil
}

func promptBody(req RunRequest) map[string]any {
	parts := []any{map[string]any{"type": "text", "text": req.Prompt}}
	for _, f := range req.Files {
		part := map[string]any{"type": "file", "mime": f.MIME, "url": f.URL}
		if f.Name != "" {
			part["filename"] = f.Name
		}
		parts = append(parts, part)
	}
	body := map[string]any{"parts": parts}
	if req.ProviderID != "" && req.ModelID != "" {
		body["model"] = map[string]string{"providerID": req.ProviderID, "modelID": req.ModelID}
	}
	if strings.TrimSpace(req.System) != "" {
		body["system"] = req.System
	}
	if req.Variant != "" {
		body["variant"] = req.Variant
	}
	if req.ClientTools && req.Schema != nil {
		body["format"] = map[string]any{"type": "json_schema", "schema": req.Schema}
	}
	return body
}

func (c *Codefree) fetchAssistant(ctx context.Context, sid string) (assistantPayload, error) {
	var msgs []assistantPayload
	err := c.do(ctx, http.MethodGet, "/session/"+url.PathEscape(sid)+"/message", c.dirQuery(), nil, &msgs)
	if err != nil {
		return assistantPayload{}, err
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Info.Role == "assistant" || msgs[i].Info.ModelID != "" {
			return msgs[i], nil
		}
	}
	if len(msgs) > 0 {
		return msgs[len(msgs)-1], nil
	}
	return assistantPayload{}, fmt.Errorf("codefree-o returned no messages")
}

type cfEvent struct {
	Type       string          `json:"type"`
	Properties json.RawMessage `json:"properties"`
}

type tokenUsage struct {
	Input     float64 `json:"input"`
	Output    float64 `json:"output"`
	Reasoning float64 `json:"reasoning"`
	Cache     struct {
		Read  float64 `json:"read"`
		Write float64 `json:"write"`
	} `json:"cache"`
}

type assistantInfo struct {
	Role       string          `json:"role"`
	ModelID    string          `json:"modelID"`
	Error      json.RawMessage `json:"error"`
	Tokens     tokenUsage      `json:"tokens"`
	Structured json.RawMessage `json:"structured"`
	Finish     string          `json:"finish"`
	Time       struct {
		Completed *float64 `json:"completed"`
	} `json:"time"`
}

type assistantPayload struct {
	Info  assistantInfo `json:"info"`
	Parts []cfPart      `json:"parts"`
}

type cfPart struct {
	Type   string          `json:"type"`
	Text   string          `json:"text"`
	Reason string          `json:"reason"`
	Tokens tokenUsage      `json:"tokens"`
	Tool   string          `json:"tool"`
	CallID string          `json:"callID"`
	State  json.RawMessage `json:"state"`
}

func (p assistantPayload) toResult(sid string, clientTools bool) RunResult {
	acc := newAccumulator(sid)
	acc.applyPayload(p, clientTools)
	return acc.result()
}

func (r RunResult) fail() error {
	if r.Text == "" && r.Reasoning == "" && len(r.Tools) == 0 && r.Finish == "error" {
		return fmt.Errorf("codefree-o: %s", r.StopMatch)
	}
	return nil
}

type accumulator struct {
	sessionID        string
	texts            map[string]*strings.Builder
	textOrder        []string
	reasons          map[string]*strings.Builder
	reasonOrder      []string
	reasoningEmitted string
	usage            Usage
	finish           string
	errMsg           string
	structured       json.RawMessage
	completed        bool
	clientTools      bool
}

func newAccumulator(sid string) *accumulator {
	return &accumulator{
		sessionID: sid,
		texts:     map[string]*strings.Builder{},
		reasons:   map[string]*strings.Builder{},
	}
}

func (a *accumulator) event(ev cfEvent, sawBusy *bool, emit func(Delta) error) (bool, error) {
	switch ev.Type {
	case "session.status":
		var p struct {
			SessionID string `json:"sessionID"`
			Status    struct {
				Type string `json:"type"`
			} `json:"status"`
		}
		if err := json.Unmarshal(ev.Properties, &p); err != nil || p.SessionID != a.sessionID {
			return false, nil
		}
		if p.Status.Type == "busy" {
			*sawBusy = true
		}
		if p.Status.Type == "idle" && *sawBusy && (a.completed || a.joinedText() != "" || a.errMsg != "") {
			return true, nil
		}
	case "session.next.text.delta":
		var p struct {
			SessionID string `json:"sessionID"`
			TextID    string `json:"textID"`
			Delta     string `json:"delta"`
		}
		if json.Unmarshal(ev.Properties, &p) != nil || p.SessionID != a.sessionID || p.Delta == "" {
			return false, nil
		}
		a.appendText(p.TextID, p.Delta, "")
		if emit != nil {
			return false, emit(Delta{Text: p.Delta})
		}
	case "session.next.text.ended":
		var p struct {
			SessionID string `json:"sessionID"`
			TextID    string `json:"textID"`
			Text      string `json:"text"`
		}
		if json.Unmarshal(ev.Properties, &p) != nil || p.SessionID != a.sessionID {
			return false, nil
		}
		extra := a.appendText(p.TextID, "", p.Text)
		if extra != "" && emit != nil {
			return false, emit(Delta{Text: extra})
		}
	case "session.next.reasoning.delta":
		var p struct {
			SessionID   string `json:"sessionID"`
			ReasoningID string `json:"reasoningID"`
			Delta       string `json:"delta"`
		}
		if json.Unmarshal(ev.Properties, &p) != nil || p.SessionID != a.sessionID || p.Delta == "" {
			return false, nil
		}
		a.appendReason(p.ReasoningID, p.Delta, "")
		a.reasoningEmitted += p.Delta
		if emit != nil {
			return false, emit(Delta{Reasoning: p.Delta})
		}
	case "session.next.step.ended":
		var p struct {
			SessionID string     `json:"sessionID"`
			Finish    string     `json:"finish"`
			Tokens    tokenUsage `json:"tokens"`
		}
		if json.Unmarshal(ev.Properties, &p) != nil || p.SessionID != a.sessionID {
			return false, nil
		}
		a.usage = usageFrom(p.Tokens)
		if p.Finish != "" {
			a.finish = mapFinish(p.Finish)
		}
		a.completed = true
	case "message.updated":
		var p struct {
			SessionID string          `json:"sessionID"`
			Info      json.RawMessage `json:"info"`
		}
		if json.Unmarshal(ev.Properties, &p) != nil || p.SessionID != a.sessionID {
			return false, nil
		}
		var info assistantInfo
		if json.Unmarshal(p.Info, &info) != nil {
			return false, nil
		}
		if info.Role != "" && info.Role != "assistant" {
			return false, nil
		}
		a.takeInfo(info)
	}
	return false, nil
}

func (a *accumulator) takeInfo(info assistantInfo) {
	if u := usageFrom(info.Tokens); u.Total() > 0 || u.CacheRead > 0 || u.CacheWrite > 0 {
		a.usage = u
	}
	if info.Finish != "" {
		a.finish = mapFinish(info.Finish)
	}
	if msg := errorMessage(info.Error); msg != "" {
		a.errMsg = msg
		a.finish = "error"
	}
	if len(bytesTrimSpace(info.Structured)) > 0 && string(bytesTrimSpace(info.Structured)) != "null" {
		a.structured = info.Structured
	}
	if info.Time.Completed != nil {
		a.completed = true
	}
}

func (a *accumulator) applyPayload(p assistantPayload, clientTools bool) {
	a.clientTools = clientTools
	a.takeInfo(p.Info)
	var text, reasoning strings.Builder
	var step Usage
	var stepFinish string
	for _, part := range p.Parts {
		switch part.Type {
		case "text":
			text.WriteString(part.Text)
		case "reasoning":
			reasoning.WriteString(part.Text)
		case "step-finish":
			step = usageFrom(part.Tokens)
			if part.Reason != "" {
				stepFinish = mapFinish(part.Reason)
			}
		}
	}
	if text.Len() > 0 {
		a.texts = map[string]*strings.Builder{}
		a.textOrder = nil
		a.appendText("final", "", text.String())
	}
	if reasoning.Len() > 0 {
		a.reasons = map[string]*strings.Builder{}
		a.reasonOrder = nil
		a.appendReason("final", "", reasoning.String())
	}
	if a.usage.Total() == 0 && a.usage.CacheRead == 0 {
		a.usage = step
	}
	if a.finish == "" && stepFinish != "" {
		a.finish = stepFinish
	}
}

func (a *accumulator) result() RunResult {
	text := a.joinedText()
	reasoning := a.joinedReason()
	var tools []ToolCall
	finish := a.finish
	if a.clientTools {
		if content, calls, ok := parseToolEnvelope(a.structured, text); ok {
			text = content
			tools = calls
			if len(calls) > 0 && (finish == "" || finish == "stop") {
				finish = "tool_calls"
			}
		}
	}
	if finish == "" {
		if len(tools) > 0 {
			finish = "tool_calls"
		} else if a.errMsg != "" {
			finish = "error"
		} else {
			finish = "stop"
		}
	}
	return RunResult{
		SessionID: a.sessionID,
		Text:      text,
		Reasoning: reasoning,
		Tools:     tools,
		Usage:     a.usage,
		Finish:    finish,
		StopMatch: a.errMsg,
	}
}

func (a *accumulator) appendText(id, delta, full string) string {
	return appendPiece(a.texts, &a.textOrder, id, delta, full)
}

func (a *accumulator) appendReason(id, delta, full string) string {
	return appendPiece(a.reasons, &a.reasonOrder, id, delta, full)
}

func appendPiece(m map[string]*strings.Builder, order *[]string, id, delta, full string) string {
	if id == "" {
		id = "default"
	}
	b := m[id]
	if b == nil {
		b = &strings.Builder{}
		m[id] = b
		*order = append(*order, id)
	}
	prev := b.String()
	if full != "" && len(full) >= len(prev) {
		b.Reset()
		b.WriteString(full)
		if strings.HasPrefix(full, prev) {
			return full[len(prev):]
		}
		if prev == "" {
			return full
		}
		return ""
	}
	b.WriteString(delta)
	return delta
}

func (a *accumulator) joinedText() string { return joinPieces(a.texts, a.textOrder) }

func (a *accumulator) joinedReason() string { return joinPieces(a.reasons, a.reasonOrder) }

func joinPieces(m map[string]*strings.Builder, order []string) string {
	var b strings.Builder
	for _, id := range order {
		if p := m[id]; p != nil {
			b.WriteString(p.String())
		}
	}
	return b.String()
}

func usageFrom(t tokenUsage) Usage {
	return Usage{
		Input:      int(t.Input),
		Output:     int(t.Output),
		Reasoning:  int(t.Reasoning),
		CacheRead:  int(t.Cache.Read),
		CacheWrite: int(t.Cache.Write),
	}
}

func mapFinish(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "tool-calls", "tool_calls", "toolcalls":
		return "tool_calls"
	case "length", "max_tokens", "max_output_tokens":
		return "length"
	case "content_filter", "content-filter":
		return "content_filter"
	case "error":
		return "error"
	default:
		return "stop"
	}
}

func errorMessage(raw json.RawMessage) string {
	raw = bytesTrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var body struct {
		Name string `json:"name"`
		Data struct {
			Message string `json:"message"`
		} `json:"data"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return truncate(string(raw), 500)
	}
	if body.Data.Message != "" {
		return body.Data.Message
	}
	if body.Message != "" {
		return body.Message
	}
	if body.Name != "" {
		return body.Name
	}
	return ""
}

func (c *Codefree) openEvents(ctx context.Context) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url("/event", nil), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	c.auth(req)
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 2000))
		return nil, &upstreamError{Status: resp.StatusCode, Body: truncate(string(buf), 500)}
	}
	return resp.Body, nil
}

func readSSE(r *bufio.Reader) (string, error) {
	var data []string
	for {
		line, err := r.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				if len(data) > 0 {
					return strings.Join(data, "\n"), nil
				}
				continue
			}
			if strings.HasPrefix(line, "data:") {
				data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) && len(data) > 0 {
				return strings.Join(data, "\n"), nil
			}
			return "", err
		}
	}
}

func (c *Codefree) do(ctx context.Context, method, path string, query url.Values, body any, dest any) error {
	var buf io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		buf = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.url(path, query), buf)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.auth(req)
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return &upstreamError{Status: resp.StatusCode, Body: truncate(strings.TrimSpace(string(raw)), 800)}
	}
	if dest == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("decode codefree-o response: %w", err)
	}
	return nil
}

func (c *Codefree) auth(req *http.Request) {
	if c.pass != "" {
		req.SetBasicAuth("codefree", c.pass)
	}
}

func (c *Codefree) url(path string, query url.Values) string {
	u := strings.TrimRight(c.base(), "/") + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

func (c *Codefree) dirQuery() url.Values {
	q := url.Values{}
	if c.Dir != "" {
		q.Set("directory", c.Dir)
	}
	return q
}

func (c *Codefree) base() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.baseURL
}

func (c *Codefree) http() *http.Client {
	if c.client != nil {
		return c.client
	}
	return http.DefaultClient
}

func (c *Codefree) bin() string {
	if c.Bin != "" {
		return c.Bin
	}
	return DefaultBin
}

func (c *Codefree) ensure(ctx context.Context) error {
	if c.external {
		return nil
	}
	c.startMu.Lock()
	defer c.startMu.Unlock()
	c.mu.Lock()
	running := c.started && c.baseURL != ""
	c.mu.Unlock()
	if running {
		return nil
	}
	return c.start(ctx)
}

func (c *Codefree) start(ctx context.Context) error {
	if c.pass == "" {
		c.pass = randomSecret()
	}
	cmd := exec.Command(c.bin(), "serve", "--pure", "--hostname", "127.0.0.1", "--port", "0")
	cmd.Dir = c.Dir
	cmd.Env = serveEnv(c.pass)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start codefree-o serve: %w", err)
	}
	ready := make(chan string, 1)
	exited := make(chan error, 1)
	var logs strings.Builder
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			line := sc.Text()
			if logs.Len() < 4000 {
				logs.WriteString(line)
				logs.WriteByte('\n')
			}
			if u := listenRE.FindStringSubmatch(line); len(u) == 2 {
				select {
				case ready <- strings.TrimRight(u[1], "/"):
				default:
				}
			}
		}
	}()
	go func() {
		exited <- cmd.Wait()
	}()
	select {
	case base := <-ready:
		c.mu.Lock()
		c.cmd = cmd
		c.baseURL = base
		c.started = true
		c.client = &http.Client{}
		c.mu.Unlock()
		go func() {
			<-exited
			c.mu.Lock()
			c.started = false
			c.baseURL = ""
			c.cmd = nil
			c.mu.Unlock()
		}()
		return nil
	case err := <-exited:
		return fmt.Errorf("codefree-o serve exited: %v %s", err, truncate(logs.String(), 500))
	case <-ctx.Done():
		killProcess(cmd)
		return ctx.Err()
	case <-time.After(20 * time.Second):
		killProcess(cmd)
		return fmt.Errorf("codefree-o serve did not listen: %s", truncate(logs.String(), 500))
	}
}

// Close stops the codefree-o serve child started by this process.
func (c *Codefree) Close() {
	c.mu.Lock()
	cmd := c.cmd
	c.started = false
	c.baseURL = ""
	c.cmd = nil
	c.mu.Unlock()
	killProcess(cmd)
}

func killProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		_ = cmd.Process.Kill()
	}
}

func serveEnv(password string) []string {
	base := childEnv()
	out := make([]string, 0, len(base)+2)
	for _, e := range base {
		if strings.HasPrefix(e, "OPENCODE_SERVER_PASSWORD=") || strings.HasPrefix(e, "OPENCODE_SERVER_USERNAME=") {
			continue
		}
		out = append(out, e)
	}
	out = append(out, "OPENCODE_SERVER_USERNAME=codefree", "OPENCODE_SERVER_PASSWORD="+password)
	return out
}

func randomSecret() string {
	var b [18]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("cf-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
