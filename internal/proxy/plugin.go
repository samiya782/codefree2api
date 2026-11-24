package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"

	"codefree2api/internal/srd"
)

// Plugin speaks the VS Code plugin websocket.
type Plugin struct {
	remote
	WSURL         string
	EditorVersion string
	PluginVersion string
	Dir           string

	editor string
	plugin string
}

func (p *Plugin) Models(ctx context.Context) ([]ModelInfo, error) {
	return p.models(ctx, true)
}

func (p *Plugin) models(ctx context.Context, retry bool) ([]ModelInfo, error) {
	sess, err := p.session(ctx)
	if err != nil {
		return nil, err
	}
	status, body, err := p.caller(sess).Do(ctx, srd.Call{
		Method:    http.MethodGet,
		Path:      modelsPath,
		ProjectID: "0",
		GitURLs:   srd.ListRemotes(ctx, p.dir()),
	})
	if err != nil {
		return nil, errUpstream
	}
	if status == http.StatusUnauthorized && retry {
		p.clear()
		return p.models(ctx, false)
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

func (p *Plugin) Run(ctx context.Context, req RunRequest, emit func(Delta) error) (RunResult, error) {
	res, err := p.run(ctx, req, emit, true)
	if err != nil {
		return RunResult{}, err
	}
	if req.ClientTools {
		if text, calls, ok := parseToolEnvelope(nil, res.Text, req.Tools); ok {
			res.Text = text
			res.Tools = calls
			if len(calls) > 0 && (res.Finish == "" || res.Finish == "stop") {
				res.Finish = "tool_calls"
			}
		}
	}
	if res.Finish == "" {
		res.Finish = "stop"
	}
	return res, nil
}

func (p *Plugin) run(ctx context.Context, req RunRequest, emit func(Delta) error, retry bool) (RunResult, error) {
	sess, err := p.session(ctx)
	if err != nil {
		return RunResult{}, err
	}
	editor, pluginVersion, err := p.versions(ctx, sess)
	if err != nil {
		if retry && err == errAuth {
			p.clear()
			return p.run(ctx, req, emit, false)
		}
		return RunResult{}, err
	}
	conn, err := srd.Dial(ctx, p.wsURL())
	if err != nil {
		return RunResult{}, errUpstream
	}
	defer conn.Close()
	if err := p.register(conn, sess, editor); err != nil {
		if retry && err == errAuth {
			p.clear()
			return p.run(ctx, req, emit, false)
		}
		return RunResult{}, err
	}
	res, err := p.chat(ctx, conn, sess, editor, pluginVersion, req, emit)
	if retry && err == errAuth {
		p.clear()
		return p.run(ctx, req, emit, false)
	}
	return res, err
}

func (p *Plugin) register(conn *srd.Conn, sess *srd.Session, editor string) error {
	payload, err := p.envelope("RegisterChannel", "", sess, editor, map[string]any{})
	if err != nil {
		return errUpstream
	}
	if err := conn.WriteChannel(payload); err != nil {
		return errUpstream
	}
	for {
		raw, err := conn.ReadChannel()
		if err != nil {
			return errUpstream
		}
		msg, ok := decodeWS(raw)
		if !ok || msg.MessageName != "RegisterChannel_resp" {
			continue
		}
		if code, present := wsInt(msg.Context["optResult"]); present && code == 401 {
			return errAuth
		}
		code, present := wsInt(msg.Context["optResult"])
		if !present || code != 0 {
			return errUpstream
		}
		return nil
	}
}

func (p *Plugin) chat(ctx context.Context, conn *srd.Conn, sess *srd.Session, editor, pluginVersion string, req RunRequest, emit func(Delta) error) (RunResult, error) {
	reqID, err := srd.Nonce(p.now())
	if err != nil {
		return RunResult{}, errUpstream
	}
	var prompts []map[string]string
	if strings.TrimSpace(req.System) != "" {
		prompts = append(prompts, map[string]string{"role": "system", "content": req.System})
	}
	prompts = append(prompts, map[string]string{"role": "user", "content": promptText(req)})
	body := map[string]any{
		"clientType":     "vscode",
		"clientVersion":  editor,
		"pluginVersion":  pluginVersion,
		"clientPlatform": clientPlatform(),
		"gitUrls":        srd.ListRemotes(ctx, p.dir()),
		"model":          req.ModelID,
		"messages": map[string]any{
			"sub_service": "assistant",
			"prompts":     prompts,
		},
	}
	payload, err := p.envelope("CodeChatRequest", reqID, sess, editor, body)
	if err != nil {
		return RunResult{}, errUpstream
	}
	if err := conn.WriteChannel(payload); err != nil {
		return RunResult{}, errUpstream
	}
	var acc string
	for {
		raw, err := conn.ReadChannel()
		if err != nil {
			return RunResult{}, errUpstream
		}
		msg, ok := decodeWS(raw)
		if !ok {
			continue
		}
		if code, present := wsInt(msg.Context["optResult"]); present && code == 401 {
			return RunResult{}, errAuth
		}
		if msg.MessageName != "CodeChatRequest_resp" {
			continue
		}
		if code, present := wsInt(msg.Payload["retCode"]); present && code != 0 {
			return RunResult{}, errUpstream
		}
		answer := scalarString(msg.Payload["answer"])
		next, delta := absorbAnswer(acc, answer)
		acc = next
		if delta != "" && emit != nil && !req.ClientTools {
			if err := emit(Delta{Text: delta}); err != nil {
				return RunResult{}, err
			}
		}
		end, present := wsInt(msg.Payload["isEnd"])
		if present && end == 1 {
			break
		}
		if b, ok := msg.Payload["isEnd"].(bool); ok && b {
			break
		}
	}
	return RunResult{Text: acc, Finish: "stop"}, nil
}

func absorbAnswer(acc, answer string) (string, string) {
	if answer == "" {
		return acc, ""
	}
	if strings.HasPrefix(answer, acc) {
		return answer, answer[len(acc):]
	}
	if strings.HasPrefix(acc, answer) {
		return acc, ""
	}
	return acc + answer, answer
}

func (p *Plugin) envelope(name, reqID string, sess *srd.Session, editor string, payload map[string]any) ([]byte, error) {
	headers, err := srd.AuthHeaders(p.App, sess, strings.ToUpper(name), p.now(), "0")
	if err != nil {
		return nil, err
	}
	ctxMap := map[string]any{
		"messageName": name,
		"invokerId":   sess.UserID,
		"version":     editor,
	}
	if name == "RegisterChannel" {
		ctxMap["appGId"] = "aicode"
	}
	if reqID != "" {
		ctxMap["reqId"] = reqID
	}
	for key, value := range headers {
		ctxMap[key] = value
	}
	return srd.Marshal(map[string]any{
		"messageName": name,
		"context":     ctxMap,
		"payload":     payload,
	})
}

func (p *Plugin) versions(ctx context.Context, sess *srd.Session) (string, string, error) {
	p.mu.Lock()
	editor, pluginVersion := p.editor, p.plugin
	p.mu.Unlock()
	if editor != "" && pluginVersion != "" {
		return editor, pluginVersion, nil
	}
	if editor == "" {
		editor = strings.TrimSpace(p.EditorVersion)
	}
	if editor == "" {
		found, err := srd.EditorVersion(ctx)
		if err != nil || found == "" {
			return "", "", errNoEditor
		}
		editor = found
	}
	if pluginVersion == "" {
		pluginVersion = strings.TrimSpace(p.PluginVersion)
	}
	if pluginVersion == "" {
		status, body, err := p.caller(sess).Do(ctx, srd.Call{
			Method:    http.MethodGet,
			Path:      detailPath,
			Query:     url.Values{"componentId": {"39999"}},
			ProjectID: "0",
		})
		if status == http.StatusUnauthorized {
			return "", "", errAuth
		}
		if err != nil || status != http.StatusOK {
			return "", "", errNoPlugin
		}
		found := srd.LatestVersion(body)
		if found == "" {
			return "", "", errNoPlugin
		}
		pluginVersion = found
	}
	p.mu.Lock()
	p.editor, p.plugin = editor, pluginVersion
	p.mu.Unlock()
	return editor, pluginVersion, nil
}

func (p *Plugin) wsURL() string {
	if p.WSURL != "" {
		return p.WSURL
	}
	return strings.TrimRight(p.App.WS, "/") + wsPath
}

func (p *Plugin) dir() string {
	if p.Dir != "" {
		return p.Dir
	}
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}

func clientPlatform() string {
	if runtime.GOOS == "windows" {
		return "Windows"
	}
	if runtime.GOOS == "linux" {
		return "Linux"
	}
	return runtime.GOOS
}

type wsMessage struct {
	MessageName string         `json:"messageName"`
	Context     map[string]any `json:"context"`
	Payload     map[string]any `json:"payload"`
}

func decodeWS(raw []byte) (wsMessage, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var msg wsMessage
	if err := dec.Decode(&msg); err != nil {
		return wsMessage{}, false
	}
	if msg.Context == nil {
		msg.Context = map[string]any{}
	}
	if msg.Payload == nil {
		msg.Payload = map[string]any{}
	}
	return msg, true
}

func wsInt(v any) (int, bool) {
	switch t := v.(type) {
	case json.Number:
		n, err := t.Int64()
		return int(n), err == nil
	case bool:
		if t {
			return 1, true
		}
		return 0, true
	default:
		return 0, false
	}
}

var (
	errNoEditor = errText("没有编辑器版本")
	errNoPlugin = errText("没有插件版本")
)

type errText string

func (e errText) Error() string { return string(e) }
