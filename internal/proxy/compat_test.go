package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeEngine struct {
	mu     sync.Mutex
	got    RunRequest
	result RunResult
	deltas []Delta
	models []ModelInfo
	err    error
}

func (f *fakeEngine) Models(context.Context) ([]ModelInfo, error) {
	return f.models, nil
}

func (f *fakeEngine) Run(_ context.Context, req RunRequest, emit func(Delta) error) (RunResult, error) {
	f.mu.Lock()
	f.got = req
	deltas := append([]Delta(nil), f.deltas...)
	res := f.result
	err := f.err
	f.mu.Unlock()
	if err != nil {
		return RunResult{}, err
	}
	if emit != nil {
		for _, d := range deltas {
			if err := emit(d); err != nil {
				return res, err
			}
		}
	}
	if res.Finish == "" {
		res.Finish = "stop"
	}
	return res, nil
}

func (f *fakeEngine) last() RunRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.got
}

func newTestServer(t *testing.T, eng Engine, password string) *Server {
	t.Helper()
	srv, err := newServer(Options{
		Port:     8000,
		Password: password,
		Bin:      "codefree-o",
		Aliases:  map[string]string{"gpt-4o-mini": "codefree/GLM-5.2"},
	}, eng)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func doJSON(t *testing.T, srv *Server, method, path, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		if strings.HasPrefix(key, "raw:") {
			req.Header.Set("x-api-key", strings.TrimPrefix(key, "raw:"))
		} else {
			req.Header.Set("Authorization", "Bearer "+key)
		}
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestChatRejectsTemperature(t *testing.T) {
	srv := newTestServer(t, &fakeEngine{}, "")
	rec := doJSON(t, srv, http.MethodPost, "/v1/chat/completions", "", `{"model":"m","temperature":3,"messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, http.MethodPost, "/v1/messages", "", `{"model":"m","temperature":1.5,"max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("anthropic status %d body %s", rec.Code, rec.Body.String())
	}
	var env map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env["type"] != "error" {
		t.Fatalf("anthropic error shape %#v", env)
	}
}

func TestAPIKeyHeaderAndAlias(t *testing.T) {
	eng := &fakeEngine{result: RunResult{Text: "pong", Usage: Usage{Input: 2, Output: 1}}}
	srv := newTestServer(t, eng, "secret")
	rec := doJSON(t, srv, http.MethodPost, "/v1/chat/completions", "", `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	rec = doJSON(t, srv, http.MethodPost, "/v1/chat/completions", "raw:secret", `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"ping"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("x-api-key status %d %s", rec.Code, rec.Body.String())
	}
	if eng.last().ModelID != "GLM-5.2" || eng.last().ProviderID != "codefree" {
		t.Fatalf("resolved model %+v", eng.last())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["model"] != "gpt-4o-mini" {
		t.Fatalf("echoed model %#v", body["model"])
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"ping"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer wrong")
	req.Header.Set("x-api-key", "secret")
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong bearer fell through, status %d", rec.Code)
	}
}

func TestStopSequenceAndUsage(t *testing.T) {
	eng := &fakeEngine{result: RunResult{Text: "hello END world", Usage: Usage{Input: 4, Output: 3, CacheRead: 2}}}
	srv := newTestServer(t, eng, "")
	rec := doJSON(t, srv, http.MethodPost, "/v1/chat/completions", "", `{"model":"codefree/GLM-5.2","stop":"END","messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	msg := body["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "hello " {
		t.Fatalf("content %#v", msg["content"])
	}
	usage := body["usage"].(map[string]any)
	if usage["prompt_tokens"].(float64) != 4 {
		t.Fatalf("usage %#v", usage)
	}
}

func TestAnthropicStreamEvents(t *testing.T) {
	eng := &fakeEngine{deltas: []Delta{{Text: "hi"}}, result: RunResult{Text: "hi", Usage: Usage{Input: 1, Output: 1}}}
	srv := newTestServer(t, eng, "")
	rec := doJSON(t, srv, http.MethodPost, "/v1/messages", "raw:ignored", `{"model":"codefree/GLM-5.2","max_tokens":16,"stream":true,"messages":[{"role":"user","content":"ping"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	body := rec.Body.String()
	for _, ev := range []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"} {
		if !strings.Contains(body, "event: "+ev+"\n") {
			t.Fatalf("missing %s in %s", ev, body)
		}
	}
	if !strings.Contains(body, `"text":"hi"`) && !strings.Contains(body, `"text": "hi"`) {
		t.Fatalf("missing text delta %s", body)
	}
}

func TestCountTokensAndResponses(t *testing.T) {
	eng := &fakeEngine{result: RunResult{Text: "pong", SessionID: "ses_1", Usage: Usage{Input: 3, Output: 1}}}
	srv := newTestServer(t, eng, "")
	rec := doJSON(t, srv, http.MethodPost, "/v1/messages/count_tokens", "", `{"model":"codefree/GLM-5.2","messages":[{"role":"user","content":"hello"}]}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "input_tokens") {
		t.Fatalf("count %d %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, srv, http.MethodPost, "/v1/responses", "", `{"model":"codefree/GLM-5.2","input":"ping"}`)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "pong") || !strings.Contains(rec.Body.String(), `"object":"response"`) && !strings.Contains(rec.Body.String(), `"object": "response"`) {
		t.Fatalf("response body %s", rec.Body.String())
	}
	var first map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	id := first["id"].(string)
	rec = doJSON(t, srv, http.MethodPost, "/v1/responses", "", `{"model":"codefree/GLM-5.2","previous_response_id":"`+id+`","input":"again"}`)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	if eng.last().SessionID != "ses_1" {
		t.Fatalf("session not continued: %+v", eng.last())
	}
	rec = doJSON(t, srv, http.MethodPost, "/v1/responses", "", `{"model":"codefree/GLM-5.2","previous_response_id":"resp_missing","input":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing previous %d", rec.Code)
	}
}

func TestClientToolsPassedUpstream(t *testing.T) {
	eng := &fakeEngine{result: RunResult{
		Text:   "",
		Finish: "tool_calls",
		Tools:  []ToolCall{{ID: "call_1", Name: "ping", Arguments: `{"x":1}`}},
		Usage:  Usage{Input: 5, Output: 2},
	}}
	srv := newTestServer(t, eng, "")
	body := `{"model":"codefree/GLM-5.2","tools":[{"type":"function","function":{"name":"ping","description":"p","parameters":{"type":"object"}}}],"messages":[{"role":"user","content":[{"type":"text","text":"go"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aGk="}}]}]}`
	rec := doJSON(t, srv, http.MethodPost, "/v1/chat/completions", "", body)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	got := eng.last()
	if !got.ClientTools || got.Schema == nil {
		t.Fatalf("tools not enabled: %+v", got)
	}
	if len(got.Files) != 1 || !strings.HasPrefix(got.Files[0].URL, "file://") {
		t.Fatalf("file %+v", got.Files)
	}
	if !strings.Contains(rec.Body.String(), `"name":"ping"`) && !strings.Contains(rec.Body.String(), `"name": "ping"`) {
		t.Fatalf("missing tool call %s", rec.Body.String())
	}
}

func TestToolChoiceNoneSkipsSchema(t *testing.T) {
	eng := &fakeEngine{result: RunResult{Text: "ok"}}
	srv := newTestServer(t, eng, "")
	body := `{"model":"m","tool_choice":"none","tools":[{"type":"function","function":{"name":"ping","parameters":{"type":"object"}}}],"messages":[{"role":"user","content":"hi"}]}`
	rec := doJSON(t, srv, http.MethodPost, "/v1/chat/completions", "", body)
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	if eng.last().ClientTools {
		t.Fatal("tool_choice none still enabled tools")
	}
}

func TestLimiterStopsBeforeMarker(t *testing.T) {
	out, finish, matched := applyLimits("hello END world", []string{"END"}, 0)
	if out != "hello " || finish != "stop_sequence" || matched != "END" {
		t.Fatalf("%q %s %s", out, finish, matched)
	}
	out, finish, _ = applyLimits("你好世界多余", nil, 2)
	if finish != "length" || EstimateTokens(out) > 2 {
		t.Fatalf("%q %s tokens %d", out, finish, EstimateTokens(out))
	}
}

func TestLaunchCommand(t *testing.T) {
	spec, err := LaunchCommand("claude", 8000, "", "codefree/GLM-5.2", []string{"--print"})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Name != "claude" || spec.Args[0] != "--model" {
		t.Fatalf("%+v", spec)
	}
	if spec.Env[1] != "ANTHROPIC_API_KEY=local" {
		t.Fatalf("%v", spec.Env)
	}
	spec, err = LaunchCommand("codex", 8000, "pw", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(spec.Args, " ")
	if !strings.Contains(joined, "wire_api=responses") || spec.Env[0] != "CODEFREE2API_OPENAI_API_KEY=pw" {
		t.Fatalf("%+v", spec)
	}
}

func TestCodefreeSyncAndStream(t *testing.T) {
	prompted := make(chan struct{})
	var once sync.Once
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "codefree" || pass != "pw" {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/session":
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "ses_test"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/prompt_async"):
			w.WriteHeader(http.StatusNoContent)
			once.Do(func() { close(prompted) })
		case r.Method == http.MethodGet && r.URL.Path == "/event":
			w.Header().Set("Content-Type", "text/event-stream")
			fl, _ := w.(http.Flusher)
			io.WriteString(w, "data: {\"type\":\"server.connected\",\"properties\":{}}\n\n")
			if fl != nil {
				fl.Flush()
			}
			select {
			case <-prompted:
			case <-r.Context().Done():
				return
			}
			events := []string{
				`{"type":"session.status","properties":{"sessionID":"ses_test","status":{"type":"busy"}}}`,
				`{"type":"session.next.text.delta","properties":{"sessionID":"ses_test","textID":"t1","delta":"po"}}`,
				`{"type":"session.next.text.delta","properties":{"sessionID":"ses_test","textID":"t1","delta":"ng"}}`,
				`{"type":"session.next.step.ended","properties":{"sessionID":"ses_test","finish":"stop","tokens":{"input":2,"output":1,"reasoning":0,"cache":{"read":0,"write":0}}}}`,
				`{"type":"session.status","properties":{"sessionID":"ses_test","status":{"type":"idle"}}}`,
			}
			for _, ev := range events {
				io.WriteString(w, "data: "+ev+"\n\n")
				if fl != nil {
					fl.Flush()
				}
			}
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/message"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"info": map[string]any{
					"role":   "assistant",
					"tokens": map[string]any{"input": 2, "output": 1, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}},
					"time":   map[string]any{"completed": 1},
				},
				"parts": []any{map[string]any{"type": "text", "text": "pong"}},
			}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/message"):
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			if _, ok := req["format"]; !ok {
				t.Errorf("expected json schema format, body %#v", req)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"info": map[string]any{
					"role":   "assistant",
					"tokens": map[string]any{"input": 4, "output": 2, "reasoning": 0, "cache": map[string]any{"read": 1, "write": 0}},
					"structured": map[string]any{
						"content":    "",
						"tool_calls": []any{map[string]any{"name": "ping", "arguments": map[string]any{"x": 1}}},
					},
					"time": map[string]any{"completed": 1},
				},
				"parts": []any{map[string]any{"type": "text", "text": "{\"content\":\"\",\"tool_calls\":[]}"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	cf := &Codefree{baseURL: upstream.URL, client: upstream.Client(), external: true, pass: "pw", Dir: t.TempDir()}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var got strings.Builder
	res, err := cf.Run(ctx, RunRequest{ProviderID: "codefree", ModelID: "GLM-5.2", Prompt: "hi"}, func(d Delta) error {
		got.WriteString(d.Text)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "pong" || res.Text != "pong" || res.Usage.Input != 2 || res.Usage.Output != 1 {
		t.Fatalf("stream got %q result %+v", got.String(), res)
	}

	toolRes, err := cf.Run(ctx, RunRequest{
		ProviderID:  "codefree",
		ModelID:     "GLM-5.2",
		Prompt:      "use the tool",
		ClientTools: true,
		Schema:      map[string]any{"type": "object"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(toolRes.Tools) != 1 || toolRes.Tools[0].Name != "ping" || toolRes.Finish != "tool_calls" {
		t.Fatalf("tools %+v", toolRes)
	}
	if toolRes.Usage.CacheRead != 1 {
		t.Fatalf("cache %+v", toolRes.Usage)
	}
}

func TestChatStreamDone(t *testing.T) {
	eng := &fakeEngine{deltas: []Delta{{Text: "yo"}}, result: RunResult{Text: "yo", Usage: Usage{Input: 1, Output: 1}}}
	srv := newTestServer(t, eng, "")
	rec := doJSON(t, srv, http.MethodPost, "/v1/chat/completions", "", `{"model":"m","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"hi"}]}`)
	body := rec.Body.String()
	if !strings.Contains(body, `"content":"yo"`) && !strings.Contains(body, `"content": "yo"`) {
		t.Fatalf("missing delta %s", body)
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "data: [DONE]") && !strings.Contains(body, "data: [DONE]\n") {
		t.Fatalf("missing DONE %s", body)
	}
	if !strings.Contains(body, "prompt_tokens") {
		t.Fatalf("missing usage %s", body)
	}
}

func TestLivePing(t *testing.T) {
	if os.Getenv("CODEFREE2API_LIVE") != "1" {
		t.Skip("set CODEFREE2API_LIVE=1 to ping a logged-in codefree-o")
	}
	cf := &Codefree{Dir: t.TempDir()}
	defer cf.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	models, err := cf.Models(ctx)
	if err != nil {
		if loginFailed(err) {
			t.Skip("codefree-o login is not available")
		}
		t.Fatal(publicUpstream(err))
	}
	if len(models) == 0 || models[0].ID == "" {
		t.Fatal("codefree-o returned no models")
	}
	provider, modelID := splitModel(models[0].ID)
	res, err := cf.Run(ctx, RunRequest{
		ProviderID: provider,
		ModelID:    modelID,
		Prompt:     "Reply with the single word pong.",
	}, func(Delta) error { return nil })
	if err != nil {
		if loginFailed(err) {
			t.Skip("codefree-o login is not available")
		}
		t.Fatal(publicUpstream(err))
	}
	if strings.TrimSpace(res.Text) == "" && strings.TrimSpace(res.Reasoning) == "" {
		t.Fatal("codefree-o returned an empty reply")
	}
	t.Logf("model=%s chars=%d usage_in=%d usage_out=%d", models[0].ID, len(res.Text), res.Usage.Input, res.Usage.Output)
}

func loginFailed(err error) bool {
	var ue *upstreamError
	if errors.As(err, &ue) && (ue.Status == http.StatusUnauthorized || ue.Status == http.StatusForbidden) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"login", "unauthorized", "not authenticated", "sign in"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

func publicUpstream(err error) string {
	var ue *upstreamError
	if errors.As(err, &ue) {
		return fmt.Sprintf("codefree-o HTTP %d", ue.Status)
	}
	msg := err.Error()
	if i := strings.Index(msg, "HTTP "); i >= 0 {
		head := msg[i:]
		if j := strings.Index(head, ":"); j >= 0 {
			return msg[:i] + head[:j]
		}
	}
	if len(msg) > 180 {
		return msg[:180]
	}
	return msg
}
