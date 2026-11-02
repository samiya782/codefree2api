package proxy

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestWriteSSEDataFormat(t *testing.T) {
	var buf bytes.Buffer
	payload := `{"object":"chat.completion.chunk","choices":[{"delta":{"content":"hi"}}]}`
	if err := WriteSSEData(&buf, payload); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	want := "data: " + payload + "\n\n"
	if got != want {
		t.Fatalf("sse frame mismatch\nwant %q\ngot  %q", want, got)
	}
}

func TestWriteSSEDoneFormat(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteSSEDone(&buf); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "data: [DONE]\n\n" {
		t.Fatalf("got %q", got)
	}
}

func TestOpenAIStreamSequence(t *testing.T) {
	var buf bytes.Buffer
	id := "chatcmpl-test"
	model := "codefree/GLM-5.2"
	first, err := openaiChunkJSON(id, model, "assistant", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	mid, err := openaiChunkJSON(id, model, "", "hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	stop := "stop"
	last, err := openaiChunkJSON(id, model, "", "", &stop)
	if err != nil {
		t.Fatal(err)
	}
	_ = WriteSSEData(&buf, first)
	_ = WriteSSEData(&buf, mid)
	_ = WriteSSEData(&buf, last)
	_ = WriteSSEDone(&buf)

	raw := buf.String()
	if !strings.HasSuffix(raw, "data: [DONE]\n\n") {
		t.Fatalf("missing DONE terminator: %q", raw)
	}
	frames := strings.Split(strings.TrimSuffix(raw, "\n\n"), "\n\n")
	if len(frames) != 4 {
		t.Fatalf("want 4 frames, got %d: %#v", len(frames), frames)
	}
	for i, f := range frames[:3] {
		if !strings.HasPrefix(f, "data: ") {
			t.Fatalf("frame %d missing data prefix: %q", i, f)
		}
		body := strings.TrimPrefix(f, "data: ")
		var obj map[string]any
		if err := json.Unmarshal([]byte(body), &obj); err != nil {
			t.Fatalf("frame %d not JSON: %v %q", i, err, body)
		}
		if obj["object"] != "chat.completion.chunk" {
			t.Fatalf("frame %d object=%v", i, obj["object"])
		}
	}
	if frames[3] != "data: [DONE]" {
		t.Fatalf("last frame %q", frames[3])
	}
}

func TestWriteSSEEventAnthropic(t *testing.T) {
	var buf bytes.Buffer
	payload := map[string]any{"type": "message_start", "message": map[string]any{"id": "msg-1"}}
	if err := WriteSSEEvent(&buf, "message_start", payload); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if !strings.HasPrefix(got, "event: message_start\n") {
		t.Fatalf("missing event line: %q", got)
	}
	if !strings.Contains(got, "data: {") {
		t.Fatalf("missing data line: %q", got)
	}
	if !strings.HasSuffix(got, "\n\n") {
		t.Fatalf("missing blank line terminator: %q", got)
	}
}
