package proxy

import (
	"encoding/json"
	"strings"
)

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
