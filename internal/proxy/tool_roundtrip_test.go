package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestClaudeToolRoundTrip(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/session":
			_, _ = w.Write([]byte(`{"id":"ses_test"}`))
		case "/session/ses_test/message":
			var body struct {
				Parts []struct{ Text string }
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Parts) == 0 {
				t.Error("missing upstream prompt")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if strings.Contains(body.Parts[0].Text, "tool_result ") {
				if !strings.Contains(body.Parts[0].Text, "lookup id=call-") ||
					!strings.Contains(body.Parts[0].Text, "fixture-result") {
					t.Error("tool call and result were not passed upstream")
				}
				_, _ = w.Write([]byte(`{"info":{"finish":"stop","structured":{"content":"fixture-result","tool_calls":[]}}}`))
				return
			}
			_, _ = w.Write([]byte(`{
				"info":{"finish":"stop","structured":{"content":"","tool_calls":[]}},
				"parts":[{"type":"text","text":"<tool_call>lookup<arg_key>key</arg_key>\"sample\"</arg_value>}"}]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	srv := newTestServer(t, &Codefree{baseURL: upstream.URL, client: upstream.Client(), external: true}, "")
	const tool = `{"name":"lookup","input_schema":{"type":"object","properties":{"key":{"type":"string"}}}}`
	first := doJSON(t, srv, http.MethodPost, "/v1/messages", "", `{"model":"m","stream":true,"max_tokens":100,
		"tools":[`+tool+`],"messages":[{"role":"user","content":"Look up sample."}]}`)
	var callID string
	var input string
	var stop string
	for _, event := range toolEvents(t, first) {
		if block := event.ContentBlock; block["type"] == "tool_use" && block["name"] == "lookup" {
			callID, _ = block["id"].(string)
		}
		delta, _ := event.Delta.(map[string]any)
		if delta["type"] == "input_json_delta" {
			input, _ = delta["partial_json"].(string)
		}
		if event.Type == "message_delta" {
			stop, _ = delta["stop_reason"].(string)
		}
	}
	if callID == "" || input != `{"key":"sample"}` || stop != "tool_use" {
		t.Fatalf("Claude did not receive a usable tool call: %s", first.Body)
	}
	second := doJSON(t, srv, http.MethodPost, "/v1/messages", "", `{"model":"m","max_tokens":100,
		"tools":[`+tool+`],"messages":[{"role":"user","content":"Look up sample."},
		{"role":"assistant","content":[{"type":"tool_use","name":"lookup","id":"`+callID+`","input":`+input+`}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"`+callID+`","content":"fixture-result"}]}]}`)
	if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), `"text":"fixture-result"`) {
		t.Fatalf("Claude result turn: %d %s", second.Code, second.Body)
	}
}

func TestCodefreeParenthesizedToolCall(t *testing.T) {
	content, calls, ok := parseToolEnvelope(nil, `Looking up the value.<tool_call>fixture.lookup({"key":"sample"})`,
		[]ToolDef{{Name: "fixture.lookup"}})
	if !ok || content != "Looking up the value." || len(calls) != 1 ||
		calls[0].Name != "fixture.lookup" || calls[0].Arguments != `{"key":"sample"}` {
		t.Fatalf("content %q calls %+v", content, calls)
	}
}

func TestCodefreeToolEnvelopeAfterProse(t *testing.T) {
	content, calls, ok := parseToolEnvelope(nil, `Looking up {"key":"sample"}.
{"content":"","tool_calls":[{"name":"lookup","arguments":{"key":"sample"}}]}`,
		[]ToolDef{{Name: "fixture.lookup"}})
	if !ok || content != "Looking up {\"key\":\"sample\"}." || len(calls) != 1 ||
		calls[0].Name != "fixture.lookup" || calls[0].Arguments != `{"key":"sample"}` {
		t.Fatalf("content %q calls %+v", content, calls)
	}
}

func TestCodefreeRetriesIncompleteToolCall(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/session" {
			_, _ = w.Write([]byte(`{"id":"ses_test"}`))
			return
		}
		var body struct {
			Agent string
			Parts []struct{ Text string }
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Parts) == 0 || body.Agent != "build" {
			t.Error("unexpected upstream request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		reply := `I will read the file. <tool_call>Read File`
		if strings.Contains(body.Parts[0].Text, "Return the complete tool request") {
			reply = `{"content":"","tool_calls":[{"name":"Read","arguments":{"file_path":"fixture.txt"}}]}`
		}
		_, _ = w.Write(raw(map[string]any{
			"info":  map[string]any{"finish": "stop", "tokens": map[string]int{"input": 10, "output": 5}},
			"parts": []map[string]string{{"type": "text", "text": reply}},
		}))
	}))
	defer upstream.Close()
	srv := newTestServer(t, &Codefree{baseURL: upstream.URL, client: upstream.Client(), external: true}, "")
	rec := doJSON(t, srv, http.MethodPost, "/v1/messages", "", `{"model":"m","max_tokens":90,
		"messages":[{"role":"user","content":"Read the fixture."}],
		"tools":[{"name":"Read","input_schema":{"type":"object","properties":{"file_path":{"type":"string"}}}}]}`)
	for _, field := range []string{`"stop_reason":"tool_use"`, `"file_path":"fixture.txt"`, `"input_tokens":20`} {
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), field) {
			t.Fatalf("missing %s in %d %s", field, rec.Code, rec.Body)
		}
	}
}

func TestCodexToolRoundTrip(t *testing.T) {
	for _, tt := range []struct {
		kind, definition, arguments, outputField, outputValue string
	}{
		{"function_call", `{"type":"function","name":"lookup","parameters":{"type":"object"}}`,
			`{"key":"sample"}`, "arguments", `{"key":"sample"}`},
		{"custom_tool_call", `{"type":"custom","name":"lookup","format":{"type":"text"}}`,
			`{"input":"sample\n"}`, "input", "sample\n"},
	} {
		t.Run(tt.kind, func(t *testing.T) {
			eng := &fakeEngine{result: RunResult{Finish: "tool_calls",
				Tools: []ToolCall{{ID: "call_fixture", Name: "fixture.lookup", Arguments: tt.arguments}}}}
			srv := newTestServer(t, eng, "")
			tools := `[{"type":"namespace","name":"fixture","tools":[` + tt.definition + `]}]`
			first := doJSON(t, srv, http.MethodPost, "/v1/responses", "", `{"model":"m","stream":true,
				"input":"Look up sample.","tools":`+tools+`}`)
			if got := eng.last(); !got.ClientTools || len(got.Tools) != 1 || got.Tools[0].Name != "fixture.lookup" {
				t.Fatalf("tool definition not offered upstream: %+v", got.Tools)
			}
			var item map[string]any
			var output []map[string]any
			for _, event := range toolEvents(t, first) {
				switch event.Type {
				case "response.output_item.done":
					if event.OutputIndex != 0 {
						t.Errorf("first item index = %d, want 0", event.OutputIndex)
					}
					item = event.Item
				case "response.completed":
					output = event.Response.Output
				}
			}
			if item["type"] != tt.kind || item["namespace"] != "fixture" || item["name"] != "lookup" ||
				item["call_id"] != "call_fixture" || item[tt.outputField] != tt.outputValue {
				t.Fatalf("Codex cannot dispatch item: %v", item)
			}
			if len(output) != 1 || !reflect.DeepEqual(item, output[0]) {
				t.Fatalf("completed response differs from streamed item: %v vs %v", output, item)
			}
			second := doJSON(t, srv, http.MethodPost, "/v1/responses", "", `{"model":"m","input":[`+
				string(raw(item))+`,{"type":"`+tt.kind+`_output","call_id":"call_fixture",
				"output":[{"type":"input_text","text":"fixture-result"}]}],"tools":`+tools+`}`)
			prompt := eng.last().Prompt
			if second.Code != http.StatusOK || !strings.Contains(prompt, "fixture.lookup") ||
				strings.Count(prompt, "call_fixture") != 2 || !strings.Contains(prompt, "tool: call_fixture: fixture-result") {
				t.Fatalf("Codex result turn: %d, upstream prompt %q", second.Code, prompt)
			}
		})
	}
}

type toolEvent struct {
	Type         string
	OutputIndex  int `json:"output_index"`
	Item         map[string]any
	ContentBlock map[string]any `json:"content_block"`
	Delta        any
	Response     struct {
		Output []map[string]any
	}
}

func toolEvents(t *testing.T, rec *httptest.ResponseRecorder) []toolEvent {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var events []toolEvent
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		var event toolEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			t.Fatalf("invalid event: %v", err)
		}
		events = append(events, event)
	}
	return events
}
