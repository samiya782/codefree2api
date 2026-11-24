package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseToolDefsNamespace(t *testing.T) {
	raws := []json.RawMessage{
		raw(map[string]any{
			"type": "namespace",
			"name": "mcp__github",
			"tools": []any{
				map[string]any{"type": "function", "name": "add_issue_comment", "parameters": map[string]any{"type": "object"}},
				map[string]any{"type": "function", "name": "create_issue", "parameters": map[string]any{"type": "object"}},
			},
		}),
		raw(map[string]any{"type": "function", "function": map[string]any{"name": "exec_command", "parameters": map[string]any{"type": "object"}}}),
		raw(map[string]any{"type": "web_search"}),
	}
	tools, err := parseToolDefs(raws, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 3 {
		t.Fatalf("tools %+v", tools)
	}
	if tools[0].Name != "mcp__github.add_issue_comment" || tools[1].Name != "mcp__github.create_issue" {
		t.Fatalf("namespace expansion %+v", tools)
	}
	if tools[2].Name != "exec_command" {
		t.Fatalf("plain tool %+v", tools[2])
	}
}

func TestWithToolInstructionsGuards(t *testing.T) {
	tools := []ToolDef{{Name: "exec_command", Parameters: raw(map[string]any{"type": "object"})}}
	got := withToolInstructions("be brief", tools, ToolChoice{})
	for _, want := range []string{
		"cannot execute anything yourself",
		`{"content": string, "tool_calls": [{"name": string, "arguments": object}]}`,
		"no invented tool results",
		"- exec_command",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	req := withToolInstructions("s", tools, ToolChoice{Mode: "required"})
	if !strings.Contains(req, "must call a tool") {
		t.Fatalf("required: %s", req)
	}
}

func nativeToolsFixture() []ToolDef {
	return []ToolDef{
		{Name: "exec_command", Parameters: raw(map[string]any{"type": "object"})},
		{Name: "multi_agent_v1.foo", Parameters: raw(map[string]any{"type": "object"})},
	}
}

func TestParseNativeToolCallsReplayShape(t *testing.T) {
	text := "我先看一下目录。\n<exec_command\n\nadditionalProperties: false\ncmd: ls -la /tmp\nworkdir: /home/samiya/Projects/codefree2api\n\nadditionalProperties: false\n\n</exec_command>"
	content, calls, ok := parseNativeToolCalls(text, knownToolNames(nativeToolsFixture()))
	if !ok || len(calls) != 1 {
		t.Fatalf("ok=%v calls=%+v", ok, calls)
	}
	if content != "我先看一下目录。" {
		t.Fatalf("content %q", content)
	}
	if calls[0].Name != "exec_command" {
		t.Fatalf("name %q", calls[0].Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Arguments), &args); err != nil {
		t.Fatal(err)
	}
	if args["cmd"] != "ls -la /tmp" || args["workdir"] != "/home/samiya/Projects/codefree2api" {
		t.Fatalf("args %v", args)
	}
}

func TestParseNativeToolCallsGenericJSON(t *testing.T) {
	text := "thinking...<tool_call>{\"name\": \"functions.exec_command\", \"arguments\": {\"cmd\": \"echo hi\"}}</tool_call>done"
	content, calls, ok := parseNativeToolCalls(text, knownToolNames(nativeToolsFixture()))
	if !ok || len(calls) != 1 {
		t.Fatalf("ok=%v calls=%+v", ok, calls)
	}
	if content != "thinking...done" {
		t.Fatalf("content %q", content)
	}
	if calls[0].Name != "exec_command" {
		t.Fatalf("name %q", calls[0].Name)
	}
	if !strings.Contains(calls[0].Arguments, "echo hi") {
		t.Fatalf("args %s", calls[0].Arguments)
	}
}

func TestParseNativeToolCallsNameArgumentLines(t *testing.T) {
	text := "<tool_call>\nname\nexec_command\narguments\n{\"cmd\": \"echo x\"}\n</tool_call>"
	_, calls, ok := parseNativeToolCalls(text, knownToolNames(nativeToolsFixture()))
	if !ok || len(calls) != 1 {
		t.Fatalf("ok=%v calls=%+v", ok, calls)
	}
	if calls[0].Name != "exec_command" || !strings.Contains(calls[0].Arguments, "echo x") {
		t.Fatalf("call %+v", calls[0])
	}
}

func TestParseNativeToolCallsArgKeyValue(t *testing.T) {
	text := "<multi_agent_v1.foo>\narg_key\ncmd\narg_value\necho pair\n</multi_agent_v1.foo>"
	_, calls, ok := parseNativeToolCalls(text, knownToolNames(nativeToolsFixture()))
	if !ok || len(calls) != 1 {
		t.Fatalf("ok=%v calls=%+v", ok, calls)
	}
	if calls[0].Name != "multi_agent_v1.foo" {
		t.Fatalf("name %q", calls[0].Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Arguments), &args); err != nil {
		t.Fatal(err)
	}
	if args["cmd"] != "echo pair" {
		t.Fatalf("args %v", args)
	}
}

func TestParseNativeToolCallsIgnoresUnknownMarks(t *testing.T) {
	text := "read <foo.txt then visit http://x.y and <b>bold</b> ok"
	_, _, ok := parseNativeToolCalls(text, knownToolNames(nativeToolsFixture()))
	if ok {
		t.Fatal("unknown markup parsed as tool call")
	}
	plain := "no markup at all"
	if _, _, ok := parseNativeToolCalls(plain, knownToolNames(nativeToolsFixture())); ok {
		t.Fatal("plain text parsed")
	}
	if _, _, ok := parseNativeToolCalls("<exec_command>cmd: x", nil); ok {
		t.Fatal("no known tools should disable native parsing")
	}
}

func TestParseToolEnvelopeNativeFallback(t *testing.T) {
	tools := nativeToolsFixture()
	text := "前言<exec_command>\ncmd: ls\nworkdir: /tmp\n</exec_command>"
	content, calls, ok := parseToolEnvelope(nil, text, tools)
	if !ok || len(calls) != 1 {
		t.Fatalf("ok=%v calls=%+v", ok, calls)
	}
	if content != "前言" {
		t.Fatalf("content %q", content)
	}
	if calls[0].Name != "exec_command" {
		t.Fatalf("name %q", calls[0].Name)
	}
}

func TestParseNativeToolCallsConsecutiveGeneric(t *testing.T) {
	text := "<tool_call>{\"name\": \"exec_command\", \"arguments\": {\"cmd\": \"echo one\"}}\n<tool_call>\n{\"name\": \"exec_command\", \"arguments\": {\"cmd\": \"echo two\"}}\n<tool_call>"
	content, calls, ok := parseNativeToolCalls(text, knownToolNames(nativeToolsFixture()))
	if !ok || len(calls) != 2 {
		t.Fatalf("ok=%v calls=%+v", ok, calls)
	}
	if content != "" {
		t.Fatalf("content %q", content)
	}
	if !strings.Contains(calls[0].Arguments, "echo one") || !strings.Contains(calls[1].Arguments, "echo two") {
		t.Fatalf("calls %+v", calls)
	}
}

func TestParseNativeToolCallsXMLAttrs(t *testing.T) {
	text := "我来实际检查一下技能的安装位置。⟨exec_command additionalProperties=\"true\" cmd=\"echo '=== 项目内 .codex/skills ==='; ls -la /home/samiya/Projects/codefree2api/.codex/skills/ 2⟩/dev/null || echo '(不存在)'\" workdir=\"/home/samiya/Projects/codefree2api\" yield_time_ms=\"10000\" /⟩"
	content, calls, ok := parseNativeToolCalls(text, knownToolNames(nativeToolsFixture()))
	if !ok || len(calls) != 1 {
		t.Fatalf("ok=%v calls=%+v", ok, calls)
	}
	if content != "我来实际检查一下技能的安装位置。" {
		t.Fatalf("content %q", content)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Arguments), &args); err != nil {
		t.Fatal(err)
	}
	cmd, _ := args["cmd"].(string)
	if !strings.Contains(cmd, "ls -la /home/samiya/Projects/codefree2api/.codex/skills/") {
		t.Fatalf("cmd %q", cmd)
	}
	if args["workdir"] != "/home/samiya/Projects/codefree2api" {
		t.Fatalf("workdir %v", args["workdir"])
	}
	if args["yield_time_ms"] != float64(10000) {
		t.Fatalf("yield_time_ms %v", args["yield_time_ms"])
	}
}

func TestParseNativeToolCallsXMLAttrsUnquoted(t *testing.T) {
	text := "<multi_agent_v1.foo>cmd=hello workdir=/tmp/>"
	_, calls, ok := parseNativeToolCalls(text, knownToolNames(nativeToolsFixture()))
	if !ok || len(calls) != 1 {
		t.Fatalf("ok=%v calls=%+v", ok, calls)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Arguments), &args); err != nil {
		t.Fatal(err)
	}
	if args["cmd"] != "hello" || args["workdir"] != "/tmp/" {
		t.Fatalf("args %v", args)
	}
}

func TestParseNativeToolCallsRelaxedUnknown(t *testing.T) {
	text := "⟨bash\n \n command cat package.json 2>/dev/null | head -20; ls -la\n \n workdir /home/samiya/Projects/codefree2api</bash>"
	content, calls, ok := parseNativeToolCalls(text, knownToolNames(nativeToolsFixture()))
	if !ok || len(calls) != 1 {
		t.Fatalf("ok=%v calls=%+v", ok, calls)
	}
	if content != "" {
		t.Fatalf("content %q", content)
	}
	if calls[0].Name != "bash" {
		t.Fatalf("name %q", calls[0].Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Arguments), &args); err != nil {
		t.Fatal(err)
	}
	cmd, _ := args["command"].(string)
	if !strings.Contains(cmd, "cat package.json") {
		t.Fatalf("command %q", cmd)
	}
	if args["workdir"] != "/home/samiya/Projects/codefree2api" {
		t.Fatalf("workdir %v", args["workdir"])
	}
}

func TestParseNativeToolCallsStrictWithoutAngle(t *testing.T) {
	text := "<a href=\"x\">plain html</a>"
	if _, _, ok := parseNativeToolCalls(text, knownToolNames(nativeToolsFixture())); ok {
		t.Fatal("html link misparsed as tool call")
	}
}

func TestParseNativeToolCallsGluedSchemaNoise(t *testing.T) {
	text := "我先检查一下。⟨exec_commandadditionalProperties: false cmd=\"echo '=== 项目 .agents/skills ==='; ls -la /home/samiya/Projects/codefree2api/.agents/skills 2>/dev/null\" shell=zsh workdir=/home/samiya/Projects/codefree2api yield_time_ms=5000}"
	content, calls, ok := parseNativeToolCalls(text, knownToolNames(nativeToolsFixture()))
	if !ok || len(calls) != 1 {
		t.Fatalf("ok=%v calls=%+v", ok, calls)
	}
	if content != "我先检查一下。" {
		t.Fatalf("content %q", content)
	}
	if calls[0].Name != "exec_command" {
		t.Fatalf("name %q", calls[0].Name)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Arguments), &args); err != nil {
		t.Fatal(err)
	}
	if args["shell"] != "zsh" || args["workdir"] != "/home/samiya/Projects/codefree2api" {
		t.Fatalf("args %v", args)
	}
}
