package proxy

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
)

type httpError struct {
	Status int
	Type   string
	Param  string
	Msg    string
}

func (e *httpError) Error() string {
	if e == nil {
		return ""
	}
	return e.Msg
}

func badRequest(param, msg string) *httpError {
	return &httpError{Status: http.StatusBadRequest, Type: "invalid_request_error", Param: param, Msg: msg}
}

type chatParsed struct {
	Turn         Turn
	Stream       bool
	IncludeUsage bool
}

type responsesParsed struct {
	Turn               Turn
	Stream             bool
	PreviousResponseID string
}

func parseChat(body []byte, tempMax float64) (chatParsed, error) {
	var req struct {
		Model               string            `json:"model"`
		Messages            []rawMessage      `json:"messages"`
		Stream              bool              `json:"stream"`
		Temperature         *float64          `json:"temperature"`
		MaxTokens           *int              `json:"max_tokens"`
		MaxCompletionTokens *int              `json:"max_completion_tokens"`
		Stop                json.RawMessage   `json:"stop"`
		Tools               []json.RawMessage `json:"tools"`
		ToolChoice          json.RawMessage   `json:"tool_choice"`
		ReasoningEffort     string            `json:"reasoning_effort"`
		Reasoning           *struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
		StreamOptions *struct {
			IncludeUsage bool `json:"include_usage"`
		} `json:"stream_options"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return chatParsed{}, badRequest("", "invalid JSON body: "+err.Error())
	}
	if err := validateTemp(req.Temperature, tempMax); err != nil {
		return chatParsed{}, badRequest("temperature", err.Error())
	}
	maxTok, err := oneMax(req.MaxCompletionTokens, req.MaxTokens)
	if err != nil {
		return chatParsed{}, badRequest("max_tokens", err.Error())
	}
	stop, err := parseStop(req.Stop)
	if err != nil {
		return chatParsed{}, badRequest("stop", err.Error())
	}
	tools, err := parseToolDefs(req.Tools, false)
	if err != nil {
		return chatParsed{}, err
	}
	choice, err := parseToolChoice(req.ToolChoice)
	if err != nil {
		return chatParsed{}, err
	}
	turn, err := transcriptFromMessages(req.Messages, "")
	if err != nil {
		return chatParsed{}, err
	}
	turn.Model = strings.TrimSpace(req.Model)
	turn.Tools = tools
	turn.ToolChoice = choice
	turn.MaxTokens = maxTok
	turn.Stop = stop
	turn.Variant = effortVariant(req.ReasoningEffort, effortOf(req.Reasoning), "", "")
	out := chatParsed{Turn: turn, Stream: req.Stream}
	if req.StreamOptions != nil {
		out.IncludeUsage = req.StreamOptions.IncludeUsage
	}
	return out, nil
}

func parseAnthropic(body []byte) (chatParsed, error) {
	var req struct {
		Model       string            `json:"model"`
		Messages    []rawMessage      `json:"messages"`
		System      json.RawMessage   `json:"system"`
		Stream      bool              `json:"stream"`
		MaxTokens   *int              `json:"max_tokens"`
		Temperature *float64          `json:"temperature"`
		Stop        json.RawMessage   `json:"stop_sequences"`
		Tools       []json.RawMessage `json:"tools"`
		ToolChoice  json.RawMessage   `json:"tool_choice"`
		Thinking    *struct {
			Type   string `json:"type"`
			Budget int    `json:"budget_tokens"`
		} `json:"thinking"`
		OutputConfig *struct {
			Effort string `json:"effort"`
		} `json:"output_config"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return chatParsed{}, badRequest("", "invalid JSON body: "+err.Error())
	}
	if err := validateTemp(req.Temperature, 1); err != nil {
		return chatParsed{}, badRequest("temperature", err.Error())
	}
	maxTok, err := oneMax(nil, req.MaxTokens)
	if err != nil {
		return chatParsed{}, badRequest("max_tokens", err.Error())
	}
	stop, err := parseStop(req.Stop)
	if err != nil {
		return chatParsed{}, badRequest("stop_sequences", err.Error())
	}
	tools, err := parseToolDefs(req.Tools, true)
	if err != nil {
		return chatParsed{}, err
	}
	choice, err := parseAnthropicToolChoice(req.ToolChoice)
	if err != nil {
		return chatParsed{}, err
	}
	turn, err := transcriptFromMessages(req.Messages, ExtractContent(req.System))
	if err != nil {
		return chatParsed{}, err
	}
	thinking := ""
	if req.Thinking != nil {
		thinking = req.Thinking.Type
	}
	effort := ""
	if req.OutputConfig != nil {
		effort = req.OutputConfig.Effort
	}
	turn.Model = strings.TrimSpace(req.Model)
	turn.Tools = tools
	turn.ToolChoice = choice
	turn.MaxTokens = maxTok
	turn.Stop = stop
	turn.Variant = effortVariant("", "", effort, thinking)
	return chatParsed{Turn: turn, Stream: req.Stream}, nil
}

func parseResponses(body []byte) (responsesParsed, error) {
	var req struct {
		Model              string            `json:"model"`
		Input              json.RawMessage   `json:"input"`
		Instructions       string            `json:"instructions"`
		Stream             bool              `json:"stream"`
		MaxOutputTokens    *int              `json:"max_output_tokens"`
		Temperature        *float64          `json:"temperature"`
		Stop               json.RawMessage   `json:"stop"`
		Tools              []json.RawMessage `json:"tools"`
		ToolChoice         json.RawMessage   `json:"tool_choice"`
		PreviousResponseID string            `json:"previous_response_id"`
		Reasoning          *struct {
			Effort string `json:"effort"`
		} `json:"reasoning"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return responsesParsed{}, badRequest("", "invalid JSON body: "+err.Error())
	}
	if err := validateTemp(req.Temperature, 2); err != nil {
		return responsesParsed{}, badRequest("temperature", err.Error())
	}
	maxTok, err := oneMax(nil, req.MaxOutputTokens)
	if err != nil {
		return responsesParsed{}, badRequest("max_output_tokens", err.Error())
	}
	stop, err := parseStop(req.Stop)
	if err != nil {
		return responsesParsed{}, badRequest("stop", err.Error())
	}
	tools, err := parseToolDefs(req.Tools, false)
	if err != nil {
		return responsesParsed{}, err
	}
	choice, err := parseToolChoice(req.ToolChoice)
	if err != nil {
		return responsesParsed{}, err
	}
	transcript, files, err := responsesTranscript(req.Input)
	if err != nil {
		return responsesParsed{}, err
	}
	turn := Turn{
		Model:      strings.TrimSpace(req.Model),
		System:     strings.TrimSpace(req.Instructions),
		Transcript: transcript,
		Files:      files,
		Tools:      tools,
		ToolChoice: choice,
		MaxTokens:  maxTok,
		Stop:       stop,
		Variant:    effortVariant("", effortOf(req.Reasoning), "", ""),
	}
	return responsesParsed{Turn: turn, Stream: req.Stream, PreviousResponseID: strings.TrimSpace(req.PreviousResponseID)}, nil
}

func effortOf(r *struct {
	Effort string `json:"effort"`
}) string {
	if r == nil {
		return ""
	}
	return r.Effort
}

func effortVariant(reasoningEffort, reasoningObj, outputEffort, thinking string) string {
	for _, e := range []string{reasoningEffort, reasoningObj, outputEffort} {
		if v := variantName(e); v != "" {
			return v
		}
	}
	switch strings.ToLower(strings.TrimSpace(thinking)) {
	case "enabled", "adaptive":
		return "high"
	default:
		return ""
	}
}

func variantName(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "minimal", "low", "medium", "high", "max", "xhigh":
		return strings.ToLower(strings.TrimSpace(effort))
	default:
		return ""
	}
}

func validateTemp(v *float64, max float64) error {
	if v == nil {
		return nil
	}
	if math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0 || *v > max {
		return fmt.Errorf("temperature must be between 0 and %g", max)
	}
	return nil
}

func oneMax(preferred, fallback *int) (int, error) {
	v := fallback
	if preferred != nil {
		v = preferred
	}
	if v == nil {
		return 0, nil
	}
	if *v < 0 {
		return 0, fmt.Errorf("must be >= 0")
	}
	return *v, nil
}

func parseStop(raw json.RawMessage) ([]string, error) {
	raw = bytesTrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		if one == "" {
			return nil, nil
		}
		return []string{one}, nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil, fmt.Errorf("stop must be a string or array of strings")
	}
	var out []string
	for _, s := range many {
		if s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

type rawMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Name       string          `json:"name"`
	ToolCallID string          `json:"tool_call_id"`
	ToolCalls  []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

func transcriptFromMessages(messages []rawMessage, system string) (Turn, error) {
	var sys strings.Builder
	var transcript strings.Builder
	var files []FileRef
	writeSys := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if sys.Len() > 0 {
			sys.WriteString("\n\n")
		}
		sys.WriteString(s)
	}
	writeBlock := func(role, text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		if transcript.Len() > 0 {
			transcript.WriteString("\n\n")
		}
		if role == "" {
			role = "user"
		}
		transcript.WriteString(role)
		transcript.WriteString(": ")
		transcript.WriteString(text)
	}
	writeSys(system)
	for _, m := range messages {
		role := strings.ToLower(strings.TrimSpace(m.Role))
		switch role {
		case "system", "developer":
			writeSys(textAndFiles(m.Content, &files))
		case "tool":
			body := textAndFiles(m.Content, &files)
			id := m.ToolCallID
			if id == "" {
				id = m.Name
			}
			writeBlock("tool", strings.TrimSpace(id+": "+body))
		default:
			text := textAndFiles(m.Content, &files)
			var extra strings.Builder
			extra.WriteString(text)
			for _, tc := range m.ToolCalls {
				if extra.Len() > 0 {
					extra.WriteString("\n")
				}
				extra.WriteString("tool_call ")
				extra.WriteString(tc.Function.Name)
				extra.WriteString(" ")
				if tc.Function.Arguments == "" {
					extra.WriteString("{}")
				} else {
					extra.WriteString(tc.Function.Arguments)
				}
			}
			if role == "" {
				role = "user"
			}
			writeBlock(role, extra.String())
		}
	}
	return Turn{System: sys.String(), Transcript: transcript.String(), Files: files}, nil
}

func textAndFiles(raw json.RawMessage, files *[]FileRef) string {
	raw = bytesTrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var parts []map[string]any
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ExtractContent(raw)
	}
	var b strings.Builder
	for _, p := range parts {
		typ, _ := p["type"].(string)
		switch typ {
		case "", "text", "input_text", "output_text":
			if t, _ := p["text"].(string); t != "" {
				b.WriteString(t)
			}
		case "image_url", "input_image", "image":
			if f, ok := fileFromPart(p); ok {
				*files = append(*files, f)
			}
		case "file", "input_file", "document":
			if f, ok := fileFromPart(p); ok {
				*files = append(*files, f)
			}
		case "tool_use":
			name, _ := p["name"].(string)
			id, _ := p["id"].(string)
			args := "{}"
			if in, ok := p["input"]; ok && in != nil {
				if buf, err := json.Marshal(in); err == nil {
					args = string(buf)
				}
			}
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString("tool_call ")
			b.WriteString(name)
			if id != "" {
				b.WriteString(" id=")
				b.WriteString(id)
			}
			b.WriteString(" ")
			b.WriteString(args)
		case "tool_result":
			id, _ := p["tool_use_id"].(string)
			body := flattenToolResult(p["content"])
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString("tool_result ")
			b.WriteString(id)
			b.WriteString(": ")
			b.WriteString(body)
		}
	}
	return b.String()
}

func flattenToolResult(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		buf, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		var files []FileRef
		return textAndFiles(buf, &files)
	}
}

func fileFromPart(p map[string]any) (FileRef, bool) {
	if u, ok := p["image_url"]; ok {
		switch t := u.(type) {
		case string:
			return FileRef{MIME: "image/png", URL: t}, t != ""
		case map[string]any:
			url, _ := t["url"].(string)
			return FileRef{MIME: "image/png", URL: url}, url != ""
		}
	}
	if src, ok := p["source"].(map[string]any); ok {
		st, _ := src["type"].(string)
		switch st {
		case "base64":
			media, _ := src["media_type"].(string)
			data, _ := src["data"].(string)
			if data == "" {
				return FileRef{}, false
			}
			if media == "" {
				media = "application/octet-stream"
			}
			name, _ := p["title"].(string)
			return FileRef{MIME: media, Name: name, URL: "data:" + media + ";base64," + data}, true
		case "url":
			url, _ := src["url"].(string)
			media, _ := src["media_type"].(string)
			if media == "" {
				media = "application/octet-stream"
			}
			name, _ := p["title"].(string)
			return FileRef{MIME: media, Name: name, URL: url}, url != ""
		}
	}
	url, _ := p["url"].(string)
	if url == "" {
		if f, ok := p["file"].(map[string]any); ok {
			url, _ = f["file_data"].(string)
			if url == "" {
				url, _ = f["url"].(string)
			}
		}
	}
	if url == "" {
		if data, _ := p["file_data"].(string); data != "" {
			url = data
		}
	}
	if url == "" {
		return FileRef{}, false
	}
	mime, _ := p["mime"].(string)
	if mime == "" {
		mime, _ = p["media_type"].(string)
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	name, _ := p["filename"].(string)
	if name == "" {
		name, _ = p["title"].(string)
	}
	return FileRef{MIME: mime, Name: name, URL: url}, true
}

func responsesTranscript(raw json.RawMessage) (string, []FileRef, error) {
	raw = bytesTrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil, badRequest("input", "input is required")
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if strings.TrimSpace(s) == "" {
			return "", nil, badRequest("input", "input is required")
		}
		return "user: " + s, nil, nil
	}
	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil {
		return "", nil, badRequest("input", "input must be a string or array")
	}
	var b strings.Builder
	var files []FileRef
	write := func(role, text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(role)
		b.WriteString(": ")
		b.WriteString(text)
	}
	for _, item := range items {
		typ, _ := item["type"].(string)
		switch typ {
		case "", "message":
			role, _ := item["role"].(string)
			if role == "" {
				role = "user"
			}
			if c, ok := item["content"]; ok {
				buf, _ := json.Marshal(c)
				write(role, textAndFiles(buf, &files))
			}
		case "input_text":
			text, _ := item["text"].(string)
			write("user", text)
		case "function_call":
			name, _ := item["name"].(string)
			args, _ := item["arguments"].(string)
			write("assistant", "tool_call "+name+" "+args)
		case "function_call_output":
			id, _ := item["call_id"].(string)
			out := fmt.Sprint(item["output"])
			write("tool", strings.TrimSpace(id+": "+out))
		case "input_image", "input_file":
			if f, ok := fileFromPart(item); ok {
				files = append(files, f)
			}
		default:
			if c, ok := item["content"]; ok {
				buf, _ := json.Marshal(c)
				role, _ := item["role"].(string)
				if role == "" {
					role = "user"
				}
				write(role, textAndFiles(buf, &files))
			}
		}
	}
	return b.String(), files, nil
}

func parseToolDefs(raws []json.RawMessage, anthropic bool) ([]ToolDef, error) {
	var out []ToolDef
	for _, raw := range raws {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, badRequest("tools", "invalid tool definition")
		}
		typ, _ := m["type"].(string)
		if !anthropic && typ != "" && typ != "function" {
			continue
		}
		name, desc, params := toolFields(m, anthropic)
		if name == "" {
			return nil, badRequest("tools", "tool name is required")
		}
		out = append(out, ToolDef{Name: name, Description: desc, Parameters: params})
	}
	return out, nil
}

func toolFields(m map[string]any, anthropic bool) (name, desc string, params json.RawMessage) {
	src := m
	if fn, ok := m["function"].(map[string]any); ok {
		src = fn
	}
	name, _ = src["name"].(string)
	desc, _ = src["description"].(string)
	key := "parameters"
	if anthropic {
		key = "input_schema"
	}
	if v, ok := src[key]; ok && v != nil {
		params, _ = json.Marshal(v)
	} else if v, ok := src["parameters"]; ok && v != nil {
		params, _ = json.Marshal(v)
	} else if v, ok := src["input_schema"]; ok && v != nil {
		params, _ = json.Marshal(v)
	}
	return name, desc, params
}

func parseToolChoice(raw json.RawMessage) (ToolChoice, error) {
	raw = bytesTrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ToolChoice{Mode: "auto"}, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		switch strings.ToLower(s) {
		case "", "auto":
			return ToolChoice{Mode: "auto"}, nil
		case "none":
			return ToolChoice{Mode: "none"}, nil
		case "required", "any":
			return ToolChoice{Mode: "required"}, nil
		default:
			return ToolChoice{}, badRequest("tool_choice", "unsupported tool_choice")
		}
	}
	var obj struct {
		Type     string `json:"type"`
		Name     string `json:"name"`
		Function *struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ToolChoice{}, badRequest("tool_choice", "invalid tool_choice")
	}
	switch strings.ToLower(obj.Type) {
	case "", "auto":
		return ToolChoice{Mode: "auto"}, nil
	case "none":
		return ToolChoice{Mode: "none"}, nil
	case "required", "any":
		return ToolChoice{Mode: "required"}, nil
	case "function", "tool":
		name := obj.Name
		if obj.Function != nil && obj.Function.Name != "" {
			name = obj.Function.Name
		}
		if name == "" {
			return ToolChoice{}, badRequest("tool_choice", "tool_choice name is required")
		}
		return ToolChoice{Mode: "tool", Name: name}, nil
	default:
		return ToolChoice{}, badRequest("tool_choice", "unsupported tool_choice")
	}
}

func parseAnthropicToolChoice(raw json.RawMessage) (ToolChoice, error) {
	raw = bytesTrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ToolChoice{Mode: "auto"}, nil
	}
	var obj struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ToolChoice{}, badRequest("tool_choice", "invalid tool_choice")
	}
	switch strings.ToLower(obj.Type) {
	case "", "auto":
		return ToolChoice{Mode: "auto"}, nil
	case "none":
		return ToolChoice{Mode: "none"}, nil
	case "any":
		return ToolChoice{Mode: "required"}, nil
	case "tool":
		if obj.Name == "" {
			return ToolChoice{}, badRequest("tool_choice", "tool_choice name is required")
		}
		return ToolChoice{Mode: "tool", Name: obj.Name}, nil
	default:
		return ToolChoice{}, badRequest("tool_choice", "unsupported tool_choice")
	}
}

func toolSchema(tools []ToolDef, choice ToolChoice) map[string]any {
	if len(tools) == 0 || choice.Mode == "none" {
		return nil
	}
	nameSchema := map[string]any{"type": "string"}
	if choice.Mode == "tool" && choice.Name != "" {
		nameSchema["enum"] = []string{choice.Name}
	}
	items := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"name":      nameSchema,
			"arguments": map[string]any{"type": "object"},
		},
		"required": []string{"name", "arguments"},
	}
	calls := map[string]any{"type": "array", "items": items}
	if choice.Mode == "required" || choice.Mode == "tool" {
		calls["minItems"] = 1
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"content":    map[string]any{"type": "string"},
			"tool_calls": calls,
		},
		"required": []string{"content", "tool_calls"},
	}
}

func withToolInstructions(system string, tools []ToolDef, choice ToolChoice) string {
	var b strings.Builder
	if strings.TrimSpace(system) != "" {
		b.WriteString(strings.TrimSpace(system))
		b.WriteString("\n\n")
	}
	b.WriteString("You are answering through an API. Do not use local tools or touch the filesystem. ")
	b.WriteString("Respond as JSON with keys content (string) and tool_calls (array of {name, arguments}). ")
	if choice.Mode == "required" || choice.Mode == "tool" {
		b.WriteString("You must call a tool. ")
	} else {
		b.WriteString("If no tool is needed, use an empty tool_calls array and put the answer in content. ")
	}
	b.WriteString("Available tools:\n")
	for _, t := range tools {
		b.WriteString("- ")
		b.WriteString(t.Name)
		if t.Description != "" {
			b.WriteString(": ")
			b.WriteString(t.Description)
		}
		if len(bytesTrimSpace(t.Parameters)) > 0 {
			b.WriteString(" parameters=")
			b.Write(t.Parameters)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func appendHint(system, hint string) string {
	system = strings.TrimSpace(system)
	if system == "" {
		return hint
	}
	return system + "\n\n" + hint
}

func splitModel(model string) (provider, id string) {
	model = strings.TrimSpace(model)
	if i := strings.IndexByte(model, '/'); i > 0 && i < len(model)-1 {
		return model[:i], model[i+1:]
	}
	return "codefree", model
}

type toolEnvelope struct {
	Content   string `json:"content"`
	ToolCalls []struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"tool_calls"`
}

func parseToolEnvelope(structured json.RawMessage, text string) (string, []ToolCall, bool) {
	if env, ok := decodeEnvelope(structured); ok {
		return env.Content, envelopeCalls(env), true
	}
	if env, ok := decodeEnvelope(json.RawMessage(strings.TrimSpace(text))); ok {
		return env.Content, envelopeCalls(env), true
	}
	if fence := fencedJSON(text); fence != "" {
		if env, ok := decodeEnvelope(json.RawMessage(fence)); ok {
			return env.Content, envelopeCalls(env), true
		}
	}
	return text, nil, false
}

func decodeEnvelope(raw json.RawMessage) (toolEnvelope, bool) {
	raw = bytesTrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return toolEnvelope{}, false
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return toolEnvelope{}, false
	}
	if _, ok := probe["tool_calls"]; !ok {
		if _, ok := probe["content"]; !ok {
			return toolEnvelope{}, false
		}
	}
	var env toolEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return toolEnvelope{}, false
	}
	return env, true
}

func envelopeCalls(env toolEnvelope) []ToolCall {
	var out []ToolCall
	for _, c := range env.ToolCalls {
		if strings.TrimSpace(c.Name) == "" {
			continue
		}
		out = append(out, ToolCall{
			ID:        newID("call"),
			Name:      c.Name,
			Arguments: argsString(c.Arguments),
		})
	}
	return out
}

func argsString(raw json.RawMessage) string {
	raw = bytesTrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "{}"
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			if json.Valid([]byte(s)) {
				return s
			}
			b, _ := json.Marshal(s)
			return string(b)
		}
	}
	if json.Valid(raw) {
		return string(raw)
	}
	return "{}"
}

func fencedJSON(s string) string {
	s = strings.TrimSpace(s)
	start := strings.Index(s, "```")
	if start < 0 {
		return ""
	}
	rest := s[start+3:]
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[i+1:]
	}
	end := strings.LastIndex(rest, "```")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

func countTurnTokens(turn Turn) int {
	n := EstimateTokens(turn.System) + EstimateTokens(turn.Transcript)
	for _, t := range turn.Tools {
		n += EstimateTokens(t.Name) + EstimateTokens(t.Description) + EstimateTokens(string(t.Parameters))
	}
	for _, f := range turn.Files {
		n += EstimateTokens(f.Name) + 16
	}
	if n == 0 {
		n = 1
	}
	return n
}
