package proxy

import (
	"encoding/json"
	"testing"
)

func raw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func TestExtractContentString(t *testing.T) {
	got := ExtractContent(raw("hello"))
	if got != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractContentParts(t *testing.T) {
	got := ExtractContent(raw([]map[string]string{
		{"type": "text", "text": "Hello "},
		{"type": "image", "text": "ignored"},
		{"type": "text", "text": "world"},
	}))
	if got != "Hello world" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractContentNullAndEmpty(t *testing.T) {
	if got := ExtractContent(nil); got != "" {
		t.Fatalf("nil: %q", got)
	}
	if got := ExtractContent(json.RawMessage("null")); got != "" {
		t.Fatalf("null: %q", got)
	}
}
