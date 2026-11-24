package proxy

import (
	"encoding/json"
	"strings"
)

func openAIFinish(finish string) string {
	switch finish {
	case "tool_calls":
		return "tool_calls"
	case "length":
		return "length"
	case "content_filter":
		return "content_filter"
	default:
		return "stop"
	}
}

func anthropicStop(finish, matched string) (string, any) {
	switch finish {
	case "tool_calls":
		return "tool_use", nil
	case "length":
		return "max_tokens", nil
	case "stop_sequence":
		return "stop_sequence", matched
	default:
		return "end_turn", nil
	}
}

func usageMap(u Usage) map[string]any {
	out := map[string]any{
		"prompt_tokens":     u.Input,
		"completion_tokens": u.Completion(),
		"total_tokens":      u.Total(),
	}
	if u.CacheRead > 0 {
		out["prompt_tokens_details"] = map[string]int{"cached_tokens": u.CacheRead}
	}
	if u.Reasoning > 0 {
		out["completion_tokens_details"] = map[string]int{"reasoning_tokens": u.Reasoning}
	}
	return out
}

func openAIMessage(text, reasoning string, tools []ToolCall) map[string]any {
	msg := map[string]any{"role": "assistant"}
	if text == "" && len(tools) > 0 {
		msg["content"] = nil
	} else {
		msg["content"] = text
	}
	if reasoning != "" {
		msg["reasoning_content"] = reasoning
	}
	if len(tools) > 0 {
		msg["tool_calls"] = openAIToolCalls(tools)
	}
	return msg
}

func openAIToolCalls(tools []ToolCall) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tc := range tools {
		out = append(out, map[string]any{
			"id":   tc.ID,
			"type": "function",
			"function": map[string]any{
				"name":      tc.Name,
				"arguments": tc.Arguments,
			},
		})
	}
	return out
}

func anthropicContent(text, reasoning string, tools []ToolCall) []map[string]any {
	var out []map[string]any
	if reasoning != "" {
		out = append(out, map[string]any{"type": "thinking", "thinking": reasoning})
	}
	if text != "" || (reasoning == "" && len(tools) == 0) {
		out = append(out, map[string]any{"type": "text", "text": text})
	}
	for _, tc := range tools {
		var input any
		if err := json.Unmarshal([]byte(tc.Arguments), &input); err != nil {
			input = map[string]any{}
		}
		out = append(out, map[string]any{
			"type":  "tool_use",
			"id":    tc.ID,
			"name":  tc.Name,
			"input": input,
		})
	}
	return out
}

func responseOutput(res RunResult, tools []ToolDef) []map[string]any {
	out := make([]map[string]any, 0, len(res.Tools)+2)
	if res.Reasoning != "" {
		out = append(out, responseReasoning(newID("rs"), res.Reasoning))
	}
	if res.Text != "" || len(res.Tools) == 0 {
		out = append(out, responseMessage(newID("msg"), res.Text))
	}
	for _, tc := range res.Tools {
		item, _, _ := responseToolItem(tc, tools)
		out = append(out, item)
	}
	return out
}

func responseReasoning(id, text string) map[string]any {
	return map[string]any{
		"type": "reasoning", "id": id,
		"summary": []map[string]any{{"type": "summary_text", "text": text}},
	}
}

func responseMessage(id, text string) map[string]any {
	return map[string]any{
		"type": "message", "id": id, "role": "assistant", "status": "completed",
		"content": []map[string]any{{"type": "output_text", "text": text, "annotations": []any{}}},
	}
}

func responseToolItem(call ToolCall, tools []ToolDef) (item map[string]any, argumentField, eventPrefix string) {
	item = map[string]any{
		"type": "function_call", "id": newID("fc"), "call_id": call.ID, "name": call.Name,
		"arguments": call.Arguments, "status": "completed",
	}
	for _, tool := range tools {
		if tool.Name != call.Name {
			continue
		}
		if tool.Namespace != "" {
			item["namespace"] = tool.Namespace
			item["name"] = strings.TrimPrefix(call.Name, tool.Namespace+".")
		}
		if tool.Custom {
			var args struct{ Input string }
			_ = json.Unmarshal([]byte(call.Arguments), &args)
			item["type"] = "custom_tool_call"
			item["input"] = args.Input
			delete(item, "arguments")
			return item, "input", "response.custom_tool_call_input"
		}
		break
	}
	return item, "arguments", "response.function_call_arguments"
}
