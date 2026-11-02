package proxy

import "encoding/json"

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

func responseOutput(text, reasoning string, tools []ToolCall) []any {
	var out []any
	if reasoning != "" {
		out = append(out, map[string]any{
			"type": "reasoning",
			"id":   newID("rs"),
			"summary": []map[string]any{
				{"type": "summary_text", "text": reasoning},
			},
		})
	}
	if text != "" || len(tools) == 0 {
		out = append(out, map[string]any{
			"type":   "message",
			"id":     newID("msg"),
			"role":   "assistant",
			"status": "completed",
			"content": []map[string]any{
				{"type": "output_text", "text": text, "annotations": []any{}},
			},
		})
	}
	for _, tc := range tools {
		out = append(out, map[string]any{
			"type":      "function_call",
			"id":        newID("fc"),
			"call_id":   tc.ID,
			"name":      tc.Name,
			"arguments": tc.Arguments,
			"status":    "completed",
		})
	}
	return out
}
