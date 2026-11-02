package proxy

import (
	"encoding/json"
	"strings"
)

// IncomingMessage is shared by OpenAI chat and Anthropic messages bodies.
type IncomingMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// ExtractContent turns a string or content-parts array into plain text.
// Only type=text (or missing type with a text field) is kept.
func ExtractContent(raw json.RawMessage) string {
	raw = bytesTrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var parts []map[string]any
	if err := json.Unmarshal(raw, &parts); err == nil {
		var b strings.Builder
		for _, p := range parts {
			typ, _ := p["type"].(string)
			if typ != "" && typ != "text" {
				continue
			}
			text, _ := p["text"].(string)
			if text == "" {
				continue
			}
			b.WriteString(text)
		}
		return b.String()
	}
	return ""
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

// MessagesToPrompt flattens chat messages into a plain-text prompt, keeping roles.
// An optional top-level system string (Anthropic) is prepended as role "system".
func MessagesToPrompt(messages []IncomingMessage, system string) string {
	var b strings.Builder
	write := func(role, content string) {
		role = strings.TrimSpace(role)
		content = strings.TrimSpace(content)
		if content == "" {
			return
		}
		if role == "" {
			role = "user"
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(role)
		b.WriteString(": ")
		b.WriteString(content)
	}
	write("system", system)
	for _, m := range messages {
		write(m.Role, ExtractContent(m.Content))
	}
	return b.String()
}
