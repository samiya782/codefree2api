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
			body := flattenToolResult(p["content"], files)
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

func flattenToolResult(v any, files *[]FileRef) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	case map[string]any:
		if content, ok := t["content"]; ok {
			return flattenToolResult(content, files)
		}
		buf, _ := json.Marshal(t)
		return string(buf)
	default:
		buf, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		return textAndFiles(buf, files)
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
		case "function_call", "custom_tool_call":
			name, _ := item["name"].(string)
			namespace, _ := item["namespace"].(string)
			id, _ := item["call_id"].(string)
			args, _ := item["arguments"].(string)
			if typ == "custom_tool_call" {
				args, _ = item["input"].(string)
			}
			if id != "" {
				id = " id=" + id
			}
			write("assistant", "tool_call "+qualifiedToolName(namespace, name)+id+" "+args)
		case "function_call_output", "custom_tool_call_output", "mcp_tool_call_output":
			id, _ := item["call_id"].(string)
			out := flattenToolResult(item["output"], &files)
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
		if !anthropic && typ == "namespace" {
			ns, _ := m["name"].(string)
			if subs, ok := m["tools"].([]any); ok && ns != "" {
				for _, sub := range subs {
					sm, _ := sub.(map[string]any)
					if sm == nil {
						continue
					}
					st, _ := sm["type"].(string)
					if st != "" && st != "function" && st != "custom" {
						continue
					}
					tool := toolDefinition(sm, false, ns, st == "custom")
					if tool.Name == "" {
						continue
					}
					out = append(out, tool)
				}
			}
			continue
		}
		if !anthropic && typ != "" && typ != "function" && typ != "custom" {
			continue
		}
		tool := toolDefinition(m, anthropic, "", !anthropic && typ == "custom")
		if tool.Name == "" {
			return nil, badRequest("tools", "tool name is required")
		}
		out = append(out, tool)
	}
	return out, nil
}

func toolDefinition(m map[string]any, anthropic bool, namespace string, custom bool) ToolDef {
	name, desc, params := toolFields(m, anthropic)
	if name == "" {
		return ToolDef{}
	}
	if custom {
		// The model uses the same object envelope for both tool types.
		params = json.RawMessage(`{"type":"object","properties":{"input":{"type":"string"}},"required":["input"]}`)
		if format, ok := m["format"].(map[string]any); ok && format["type"] == "grammar" {
			if grammar, ok := format["definition"].(string); ok {
				desc = strings.TrimSpace(desc + "\nInput grammar:\n" + grammar)
			}
		}
	}
	return ToolDef{
		Name: qualifiedToolName(namespace, name), Description: desc, Parameters: params,
		Namespace: namespace, Custom: custom,
	}
}

func qualifiedToolName(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "." + name
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
		Type      string `json:"type"`
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		Function  *struct {
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
	case "function", "tool", "custom":
		name := obj.Name
		if obj.Function != nil && obj.Function.Name != "" {
			name = obj.Function.Name
		}
		if name == "" {
			return ToolChoice{}, badRequest("tool_choice", "tool_choice name is required")
		}
		return ToolChoice{Mode: "tool", Name: qualifiedToolName(obj.Namespace, name)}, nil
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

func withToolInstructions(system string, tools []ToolDef, choice ToolChoice) string {
	var b strings.Builder
	if strings.TrimSpace(system) != "" {
		b.WriteString(strings.TrimSpace(system))
		b.WriteString("\n\n")
	}
	b.WriteString("You are answering through an API and you cannot execute anything yourself. ")
	b.WriteString(`Respond with a single JSON object of the form {"content": string, "tool_calls": [{"name": string, "arguments": object}]}. `)
	b.WriteString("To use a tool, put its name and arguments in tool_calls; nothing else happens. ")
	if choice.Mode == "required" || choice.Mode == "tool" {
		b.WriteString("You must call a tool: tool_calls must contain at least one entry. ")
		if choice.Mode == "tool" {
			fmt.Fprintf(&b, "Use only the tool named %q. ", choice.Name)
		}
	} else {
		b.WriteString("If no tool is needed, use an empty tool_calls array and put the answer in content. ")
	}
	b.WriteString("Use only this JSON format, with no markup, no command transcripts, and no invented tool results. ")
	b.WriteString("A tool's output can only reach you in a later turn. ")
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
			b.Write(stripSchemaDesc(t.Parameters))
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

func parseToolEnvelope(structured json.RawMessage, text string, tools []ToolDef) (string, []ToolCall, bool) {
	known := knownToolNames(tools)
	var fallback *toolEnvelope
	candidates := []json.RawMessage{structured, json.RawMessage(text), json.RawMessage(fencedJSON(text))}
	for _, candidate := range candidates {
		if env, ok := decodeEnvelope(candidate); ok {
			if calls := envelopeCalls(env, known); len(calls) > 0 {
				return env.Content, calls, true
			}
			if content, calls, ok := parseNativeToolCalls(env.Content, known); ok {
				return content, calls, true
			}
			if fallback == nil {
				fallback = &env
			}
		}
	}
	if prefix, env, ok := trailingToolEnvelope(text); ok {
		if calls := envelopeCalls(env, known); len(calls) > 0 {
			content := strings.TrimSpace(env.Content)
			if content == "" {
				content = strings.TrimSpace(prefix)
			}
			return content, calls, true
		}
	}
	if content, calls, ok := parseNativeToolCalls(text, known); ok {
		return content, calls, true
	}
	if fallback != nil {
		return fallback.Content, nil, true
	}
	return text, nil, false
}

func trailingToolEnvelope(text string) (string, toolEnvelope, bool) {
	end := strings.LastIndex(text, `"tool_calls"`)
	for end > 0 {
		start := strings.LastIndexByte(text[:end], '{')
		if start < 0 {
			break
		}
		if env, ok := decodeEnvelope(json.RawMessage(text[start:])); ok && len(env.ToolCalls) > 0 {
			return text[:start], env, true
		}
		end = start
	}
	return "", toolEnvelope{}, false
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

func envelopeCalls(env toolEnvelope, known map[string]string) []ToolCall {
	var out []ToolCall
	for _, c := range env.ToolCalls {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		if canonical, ok := canonicalToolName(name, known); ok {
			name = canonical
		}
		out = append(out, ToolCall{
			ID:        newID("call"),
			Name:      name,
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

func knownToolNames(tools []ToolDef) map[string]string {
	if len(tools) == 0 {
		return nil
	}
	m := make(map[string]string, len(tools)*2)
	add := func(alias, canonical string) {
		if alias == "" {
			return
		}
		if _, ok := m[alias]; !ok {
			m[alias] = canonical
		}
	}
	for _, t := range tools {
		add(t.Name, t.Name)
		if i := strings.LastIndexByte(t.Name, '.'); i > 0 && i < len(t.Name)-1 {
			add(t.Name[i+1:], t.Name)
		}
	}
	return m
}

func canonicalToolName(name string, known map[string]string) (string, bool) {
	for {
		if canonical, ok := known[name]; ok {
			return canonical, true
		}
		i := strings.IndexByte(name, '.')
		if i < 0 || i >= len(name)-1 {
			return "", false
		}
		name = name[i+1:]
	}
}

func genericToolMark(name string) bool {
	switch name {
	case "tool_call", "tool_calls", "tool-call", "TOOL_CALLS", "ToolCalls", "tool", "function-call", "function_call", "functioncall":
		return true
	}
	return false
}

// parseNativeToolCalls recovers tool calls when a model emits its native
// tool-call markup (e.g. "<tool_name>args</tool_name>", "<tool_call>{json}",
// "⟨tool_name⟩arg_key/arg_value blocks") instead of the requested JSON
// envelope. known maps every acceptable name (full and short) to the
// canonical ToolDef.Name.
func parseNativeToolCalls(text string, known map[string]string) (string, []ToolCall, bool) {
	if len(known) == 0 || !strings.ContainsAny(text, "<⟨") {
		return "", nil, false
	}
	// Angle brackets are a strong signal of the model's native tool-call
	// template; under that signal accept unknown tool names too and let the
	// caller report them so the model can self-correct on the next turn.
	relaxed := strings.Contains(text, "⟨")
	text = strings.ReplaceAll(text, "⟨", "<")
	text = strings.ReplaceAll(text, "⟩", ">")
	var calls []ToolCall
	var keep []string
	pos := 0
	for pos < len(text) {
		rel := strings.IndexByte(text[pos:], '<')
		if rel < 0 {
			keep = append(keep, text[pos:])
			break
		}
		start := pos + rel
		name, after, ok := readNativeMarker(text, start)
		if !ok {
			keep = append(keep, text[pos:start+1])
			pos = start + 1
			continue
		}
		end, next := findNativeCallEnd(text, after, known)
		if genericToolMark(name) {
			if call, n, ok := parseNamedNativeCall(text[after:], known); ok {
				keep = append(keep, text[pos:start])
				calls = append(calls, call)
				pos = after + n
				continue
			}
			if got := parseGenericToolCalls(text[after:end], known); len(got) > 0 {
				keep = append(keep, text[pos:start])
				calls = append(calls, got...)
				pos = next
				continue
			}
		} else {
			canonical, knownName := canonicalToolName(name, known)
			if !knownName {
				if prefix, ok := longestKnownPrefix(name, known); ok {
					canonical, knownName = prefix, true
				}
			}
			if !knownName && relaxed && looksLikeToolName(name) {
				canonical, knownName = name, true
			}
			if knownName {
				if after < len(text) && (isSpaceByte(text[after]) || xmlAttrAhead(text, after)) {
					if args, next, ok := parseXMLCall(text, after); ok {
						keep = append(keep, text[pos:start])
						calls = append(calls, ToolCall{
							ID:        newID("call"),
							Name:      canonical,
							Arguments: string(args),
						})
						pos = next
						continue
					}
				}
				if args, ok := parseNativeArgs(text[after:end]); ok {
					keep = append(keep, text[pos:start])
					calls = append(calls, ToolCall{
						ID:        newID("call"),
						Name:      canonical,
						Arguments: string(args),
					})
					pos = next
					continue
				}
			}
		}
		if len(calls) > 0 && end == len(text) {
			keep = append(keep, text[pos:start])
			pos = next
			continue
		}
		keep = append(keep, text[pos:after])
		pos = after
	}
	if len(calls) == 0 {
		return "", nil, false
	}
	content := strings.TrimSpace(strings.Join(keep, ""))
	return content, calls, true
}

// Some models spell the name directly after <tool_call>, followed by a JSON
// object in parentheses or by XML argument pairs.
func parseNamedNativeCall(s string, known map[string]string) (ToolCall, int, bool) {
	rawName, rest := splitNativeToolName(s)
	name, ok := canonicalToolName(rawName, known)
	if !ok {
		return ToolCall{}, 0, false
	}
	rest = strings.TrimLeft(rest, " \t\r\n")
	var args json.RawMessage
	if value, ok := strings.CutPrefix(rest, "("); ok {
		value = strings.TrimLeft(value, " \t\r\n")
		value, _ = strings.CutPrefix(value, "arguments=")
		dec := json.NewDecoder(strings.NewReader(value))
		if err := dec.Decode(&args); err != nil || args[0] != '{' {
			return ToolCall{}, 0, false
		}
		rest, ok = strings.CutPrefix(strings.TrimLeft(value[dec.InputOffset():], " \t\r\n"), ")")
		if !ok {
			return ToolCall{}, 0, false
		}
	} else {
		var ok bool
		args, rest, ok = parseXMLArgumentPairs(rest)
		if !ok {
			return ToolCall{}, 0, false
		}
	}
	closing := strings.TrimLeft(rest, " \t\r\n")
	for _, tag := range []string{"</tool_call>", "</function_call>", "}"} {
		if tail, ok := strings.CutPrefix(closing, tag); ok {
			rest = tail
			break
		}
	}
	return ToolCall{ID: newID("call"), Name: name, Arguments: string(args)}, len(s) - len(rest), true
}

func splitNativeToolName(s string) (string, string) {
	s = strings.TrimLeft(s, " \t\r\n")
	i := 0
	for i < len(s) && (isIdentChar(s[i]) || s[i] == '.') {
		i++
	}
	return s[:i], s[i:]
}

func incompleteNativeToolCall(text string, tools []ToolDef) bool {
	const marker = "<tool_call>"
	i := strings.LastIndex(text, marker)
	if i < 0 {
		return false
	}
	name, tail := splitNativeToolName(text[i+len(marker):])
	if strings.Contains(tail, "</tool_call>") {
		return false
	}
	_, ok := canonicalToolName(name, knownToolNames(tools))
	return ok
}

func parseXMLArgumentPairs(s string) (json.RawMessage, string, bool) {
	args := map[string]any{}
	for {
		pair, ok := strings.CutPrefix(strings.TrimLeft(s, " \t\r\n"), "<arg_key>")
		if !ok {
			break
		}
		key, value, ok := strings.Cut(pair, "</arg_key>")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, s, false
		}
		value = strings.TrimLeft(value, " \t\r\n")
		value, _ = strings.CutPrefix(value, "<arg_value>")
		value, tail, ok := strings.Cut(value, "</arg_value>")
		if !ok {
			return nil, s, false
		}
		args[strings.TrimSpace(key)] = nativeValue(xmlUnescape(value))
		s = tail
	}
	if len(args) == 0 {
		return nil, s, false
	}
	data, ok := marshalObj(args)
	return data, s, ok
}

func readNativeMarker(text string, start int) (string, int, bool) {
	i := start + 1
	if i < len(text) && text[i] == '=' {
		i++
	}
	nameStart := i
	for i < len(text) {
		c := text[i]
		switch c {
		case '>', '<':
			if c == '<' {
				return "", 0, false
			}
			name := strings.TrimSpace(text[nameStart:i])
			if name == "" {
				return "", 0, false
			}
			return name, i + 1, true
		case ' ', '\t', '\n', '\r':
			name := strings.TrimSpace(text[nameStart:i])
			if name == "" {
				return "", 0, false
			}
			return name, i, true
		}
		i++
	}
	return "", 0, false
}

func findNativeCallEnd(text string, from int, known map[string]string) (int, int) {
	for i := from; i < len(text); i++ {
		if text[i] != '<' {
			continue
		}
		if strings.HasPrefix(text[i:], "</") {
			if j := strings.IndexByte(text[i:], '>'); j >= 0 {
				return i, i + j + 1
			}
			return len(text), len(text)
		}
		if name, _, ok := readNativeMarker(text, i); ok {
			if _, knownName := canonicalToolName(name, known); knownName || genericToolMark(name) {
				return i, i
			}
		}
	}
	return len(text), len(text)
}

func parseNativeArgs(s string) (json.RawMessage, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return json.RawMessage("{}"), true
	}
	if strings.HasPrefix(s, "{") && json.Valid([]byte(s)) {
		return json.RawMessage(s), true
	}
	obj := map[string]any{}
	if parseArgKeyValues(s, obj) {
		return marshalObj(obj)
	}
	if parseColonLines(s, obj) > 0 {
		return marshalObj(obj)
	}
	if parseSpaceLines(s, obj) > 0 {
		return marshalObj(obj)
	}
	return nil, false
}

// parseXMLCall scans an XML-attribute style argument list (name is already
// consumed; text[from] is the first byte after it, expected whitespace).
// It is quote-aware so attribute values may contain <, > or newlines.
// Returns the arguments object and the offset just past the call.
func parseXMLCall(text string, from int) (json.RawMessage, int, bool) {
	obj := map[string]any{}
	i := from
	sawArg := false
	for i < len(text) {
		c := text[i]
		if isSpaceByte(c) {
			i++
			continue
		}
		if c == '/' {
			if i+1 < len(text) && text[i+1] == '>' {
				if !sawArg {
					return nil, 0, false
				}
				raw, ok := marshalObj(obj)
				if !ok {
					return nil, 0, false
				}
				return raw, i + 2, true
			}
			i++
			continue
		}
		if c == '>' {
			if !sawArg {
				return nil, 0, false
			}
			raw, ok := marshalObj(obj)
			if !ok {
				return nil, 0, false
			}
			return raw, i + 1, true
		}
		if !isIdentStart(c) {
			i++
			continue
		}
		ks := i
		for i < len(text) && isIdentChar(text[i]) {
			i++
		}
		key := text[ks:i]
		for i < len(text) && isSpaceByte(text[i]) {
			i++
		}
		if i >= len(text) || text[i] != '=' {
			continue
		}
		i++
		for i < len(text) && isSpaceByte(text[i]) {
			i++
		}
		if i >= len(text) {
			obj[key] = ""
			sawArg = true
			break
		}
		q := text[i]
		if q == '"' || q == '\'' {
			i++
			vs := i
			for i < len(text) && text[i] != q {
				i++
			}
			if i >= len(text) {
				obj[key] = nativeValue(xmlUnescape(text[vs:]))
				sawArg = true
				break
			}
			obj[key] = nativeValue(xmlUnescape(text[vs:i]))
			sawArg = true
			i++
			continue
		}
		vs := i
		for i < len(text) && !isSpaceByte(text[i]) && text[i] != '>' {
			i++
		}
		obj[key] = nativeValue(xmlUnescape(text[vs:i]))
		sawArg = true
	}
	if !sawArg {
		return nil, 0, false
	}
	raw, ok := marshalObj(obj)
	if !ok {
		return nil, 0, false
	}
	return raw, i, true
}

func xmlUnescape(s string) string {
	if !strings.Contains(s, "&") && !strings.Contains(s, `\`) {
		return s
	}
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&apos;", "'")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.ReplaceAll(s, `\'`, `'`)
	return s
}

func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// xmlAttrAhead reports whether the bytes at from look like the start of an
// XML-attribute list: identifier followed by '='.
func xmlAttrAhead(text string, from int) bool {
	i := from
	for i < len(text) && isSpaceByte(text[i]) {
		i++
	}
	if i >= len(text) || !isIdentStart(text[i]) {
		return false
	}
	for i < len(text) && isIdentChar(text[i]) {
		i++
	}
	for i < len(text) && isSpaceByte(text[i]) {
		i++
	}
	return i < len(text) && text[i] == '='
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentChar(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9') || c == '-'
}

func parseArgKeyValues(s string, obj map[string]any) bool {
	lines := strings.Split(s, "\n")
	found := false
	i := 0
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) != "arg_key" {
			i++
			continue
		}
		i++
		if i >= len(lines) {
			break
		}
		key := strings.TrimSpace(lines[i])
		i++
		if i < len(lines) && strings.TrimSpace(lines[i]) == "arg_value" {
			i++
		}
		var vals []string
		for i < len(lines) && strings.TrimSpace(lines[i]) != "arg_key" {
			vals = append(vals, lines[i])
			i++
		}
		val := strings.TrimSpace(strings.Join(vals, "\n"))
		if key != "" && val != "" {
			obj[key] = nativeValue(val)
			found = true
		}
	}
	return found
}

func parseColonLines(s string, obj map[string]any) int {
	n := 0
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		i := strings.IndexByte(ln, ':')
		if i <= 0 || i == len(ln)-1 {
			continue
		}
		key := strings.TrimSpace(ln[:i])
		val := strings.TrimSpace(ln[i+1:])
		if !isIdent(key) || val == "" {
			continue
		}
		obj[key] = nativeValue(val)
		n++
	}
	return n
}

func parseGenericToolCalls(s string, known map[string]string) []ToolCall {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var items []map[string]json.RawMessage
	if strings.HasPrefix(s, "[") {
		var arr []map[string]json.RawMessage
		if json.Unmarshal([]byte(s), &arr) == nil {
			items = arr
		}
	} else if strings.HasPrefix(s, "{") && json.Valid([]byte(s)) {
		var obj map[string]json.RawMessage
		if json.Unmarshal([]byte(s), &obj) == nil {
			items = append(items, obj)
		}
	} else {
		items = parseNameArgumentLines(s)
	}
	var out []ToolCall
	for _, item := range items {
		nameRaw, ok := item["name"]
		if !ok {
			continue
		}
		var name string
		if json.Unmarshal(nameRaw, &name) != nil || name == "" {
			continue
		}
		canonical, knownName := canonicalToolName(name, known)
		if !knownName {
			continue
		}
		args := "{}"
		if raw, ok := item["arguments"]; ok {
			var v any
			if json.Unmarshal(raw, &v) == nil {
				b, _ := json.Marshal(v)
				args = string(b)
			}
		} else if len(item) > 1 {
			flat := map[string]json.RawMessage{}
			for k, v := range item {
				if k != "name" {
					flat[k] = v
				}
			}
			if len(flat) > 0 {
				b, _ := json.Marshal(flat)
				args = string(b)
			}
		}
		out = append(out, ToolCall{ID: newID("call"), Name: canonical, Arguments: args})
	}
	return out
}

func parseNameArgumentLines(s string) []map[string]json.RawMessage {
	lines := strings.Split(s, "\n")
	var out []map[string]json.RawMessage
	var cur map[string]json.RawMessage
	flush := func() {
		if cur != nil {
			out = append(out, cur)
			cur = nil
		}
	}
	for i := 0; i < len(lines); i++ {
		key := strings.TrimSpace(lines[i])
		if key == "name" && i+1 < len(lines) {
			flush()
			cur = map[string]json.RawMessage{"name": json.RawMessage(quotedString(strings.TrimSpace(lines[i+1])))}
			i++
			continue
		}
		if key == "arguments" && cur != nil {
			var rest []string
			for j := i + 1; j < len(lines); j++ {
				if strings.TrimSpace(lines[j]) == "name" {
					break
				}
				rest = append(rest, lines[j])
			}
			raw := strings.TrimSpace(strings.Join(rest, "\n"))
			if raw != "" && json.Valid([]byte(raw)) {
				cur["arguments"] = json.RawMessage(raw)
			}
			flush()
			i += len(rest)
		}
	}
	flush()
	return out
}

func nativeValue(v string) any {
	t := strings.TrimSpace(v)
	if json.Valid([]byte(t)) {
		var out any
		if err := json.Unmarshal([]byte(t), &out); err == nil {
			return out
		}
	}
	return t
}

func marshalObj(obj map[string]any) (json.RawMessage, bool) {
	b, err := json.Marshal(obj)
	if err != nil {
		return nil, false
	}
	return b, true
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '_' || c == '-':
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

func quotedString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func looksLikeToolName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c == '_':
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}

// parseSpaceLines parses "key value" lines (space-separated, no colon) as a
// last-resort fallback inside a native call block.
func parseSpaceLines(s string, obj map[string]any) int {
	n := 0
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		i := strings.IndexByte(ln, ' ')
		if i <= 0 {
			continue
		}
		key := ln[:i]
		val := strings.TrimSpace(ln[i+1:])
		if !isIdent(key) || val == "" {
			continue
		}
		if _, exists := obj[key]; exists {
			continue
		}
		obj[key] = nativeValue(val)
		n++
	}
	return n
}

// stripSchemaDesc drops "description" fields from a JSON schema before it is
// inlined into the system prompt: parameter names are self-descriptive and
// descriptions dominate the token budget for large tool sets.
func stripSchemaDesc(raw json.RawMessage) json.RawMessage {
	if len(bytesTrimSpace(raw)) == 0 {
		return raw
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	b, err := json.Marshal(stripDescValue(v))
	if err != nil {
		return raw
	}
	return b
}

func stripDescValue(o any) any {
	switch v := o.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, val := range v {
			if k == "description" {
				continue
			}
			out[k] = stripDescValue(val)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, val := range v {
			out[i] = stripDescValue(val)
		}
		return out
	default:
		return o
	}
}

// longestKnownPrefix matches names like "exec_commandadditionalProperties:"
// where the model concatenated a known tool name with schema noise. The
// remainder must start with an identifier character (true concatenation).
func longestKnownPrefix(name string, known map[string]string) (string, bool) {
	best := ""
	for k := range known {
		if len(k) > len(best) && strings.HasPrefix(name, k) {
			rest := name[len(k):]
			if rest == "" || isIdentStart(rest[0]) {
				best = k
			}
		}
	}
	if best == "" {
		return "", false
	}
	return known[best], true
}
