package proxy

import (
	"encoding/json"
	"strings"
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

func TestMessagesToPromptKeepsRoles(t *testing.T) {
	msgs := []IncomingMessage{
		{Role: "system", Content: raw("you are a tester")},
		{Role: "user", Content: raw("ping")},
		{Role: "assistant", Content: raw("pong")},
		{Role: "user", Content: raw([]map[string]string{
			{"type": "text", "text": "and "},
			{"type": "text", "text": "again"},
		})},
	}
	got := MessagesToPrompt(msgs, "")
	want := strings.Join([]string{
		"system: you are a tester",
		"user: ping",
		"assistant: pong",
		"user: and again",
	}, "\n\n")
	if got != want {
		t.Fatalf("prompt mismatch\nwant:\n%s\n\ngot:\n%s", want, got)
	}
}

func TestMessagesToPromptPrependsAnthropicSystem(t *testing.T) {
	msgs := []IncomingMessage{
		{Role: "user", Content: raw("hi")},
	}
	got := MessagesToPrompt(msgs, "be brief")
	if !strings.HasPrefix(got, "system: be brief") {
		t.Fatalf("missing system prefix: %q", got)
	}
	if !strings.Contains(got, "user: hi") {
		t.Fatalf("missing user: %q", got)
	}
}

func TestMessagesToPromptSkipsEmpty(t *testing.T) {
	msgs := []IncomingMessage{
		{Role: "user", Content: raw("")},
		{Role: "user", Content: raw("ok")},
	}
	got := MessagesToPrompt(msgs, "  ")
	if got != "user: ok" {
		t.Fatalf("got %q", got)
	}
}
