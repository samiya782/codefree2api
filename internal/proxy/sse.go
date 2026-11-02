package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// WriteSSEData writes one OpenAI-style SSE data frame: "data: {payload}\n\n".
func WriteSSEData(w io.Writer, payload string) error {
	_, err := fmt.Fprintf(w, "data: %s\n\n", payload)
	return err
}

// WriteSSEDone writes the terminating OpenAI SSE frame.
func WriteSSEDone(w io.Writer) error {
	_, err := io.WriteString(w, "data: [DONE]\n\n")
	return err
}

// WriteSSEEvent writes an Anthropic-style SSE event with event name + data.
func WriteSSEEvent(w io.Writer, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	return err
}

func flushOf(w http.ResponseWriter) http.Flusher {
	f, _ := w.(http.Flusher)
	return f
}

func flush(f http.Flusher) {
	if f != nil {
		f.Flush()
	}
}

func openaiChunkJSON(id, model, role, content string, finish *string) (string, error) {
	delta := map[string]any{}
	if role != "" {
		delta["role"] = role
	}
	if content != "" || role != "" {
		delta["content"] = content
	}
	chunk := map[string]any{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": createdUnix(),
		"model":   model,
		"choices": []map[string]any{
			{
				"index":         0,
				"delta":         delta,
				"finish_reason": finish,
			},
		},
	}
	b, err := json.Marshal(chunk)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
