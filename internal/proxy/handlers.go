package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(w, r)
	if err != nil {
		writeHTTPError(w, false, err)
		return
	}
	parsed, err := parseChat(body, 2)
	if err != nil {
		writeHTTPError(w, false, err)
		return
	}
	if parsed.Stream {
		s.streamOpenAI(w, r, parsed.Turn, parsed.IncludeUsage)
		return
	}
	s.finishOpenAI(w, r, parsed.Turn)
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(w, r)
	if err != nil {
		writeHTTPError(w, true, err)
		return
	}
	parsed, err := parseAnthropic(body)
	if err != nil {
		writeHTTPError(w, true, err)
		return
	}
	if parsed.Stream {
		s.streamAnthropic(w, r, parsed.Turn)
		return
	}
	s.finishAnthropic(w, r, parsed.Turn)
}

func (s *Server) handleCountTokens(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(w, r)
	if err != nil {
		writeHTTPError(w, true, err)
		return
	}
	parsed, err := parseAnthropic(body)
	if err != nil {
		writeHTTPError(w, true, err)
		return
	}
	if strings.TrimSpace(parsed.Turn.Model) == "" {
		writeHTTPError(w, true, badRequest("model", "model is required"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"input_tokens": countTurnTokens(parsed.Turn)})
}

func (s *Server) handleResponses(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(w, r)
	if err != nil {
		writeHTTPError(w, false, err)
		return
	}
	parsed, err := parseResponses(body)
	if err != nil {
		writeHTTPError(w, false, err)
		return
	}
	if parsed.PreviousResponseID != "" {
		sid, ok := s.lookupSession(parsed.PreviousResponseID)
		if !ok {
			writeHTTPError(w, false, badRequest("previous_response_id", "unknown previous_response_id"))
			return
		}
		parsed.Turn.SessionID = sid
	}
	if parsed.Stream {
		s.streamResponses(w, r, parsed.Turn)
		return
	}
	s.finishResponses(w, r, parsed.Turn)
}

func (s *Server) finishOpenAI(w http.ResponseWriter, r *http.Request, turn Turn) {
	ctx, cancel := context.WithTimeout(r.Context(), runTimeout)
	defer cancel()
	start := time.Now()
	res, err := s.execute(ctx, turn, nil)
	if err != nil {
		s.note("/v1/chat/completions", turn.Model, false, err.Error(), time.Since(start), Usage{})
		writeHTTPError(w, false, err)
		return
	}
	res = limitResult(res, turn)
	s.note("/v1/chat/completions", turn.Model, true, "", time.Since(start), res.Usage)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      newID("chatcmpl"),
		"object":  "chat.completion",
		"created": createdUnix(),
		"model":   turn.Model,
		"choices": []map[string]any{{
			"index":         0,
			"message":       openAIMessage(res.Text, res.Reasoning, res.Tools),
			"finish_reason": openAIFinish(res.Finish),
		}},
		"usage": usageMap(res.Usage),
	})
}

func (s *Server) streamOpenAI(w http.ResponseWriter, r *http.Request, turn Turn, includeUsage bool) {
	if strings.TrimSpace(turn.Model) == "" {
		writeHTTPError(w, false, badRequest("model", "model is required"))
		return
	}
	fl := flushOf(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flush(fl)

	id := newID("chatcmpl")
	if first, err := openaiChunkJSON(id, turn.Model, "assistant", "", nil); err == nil {
		_ = WriteSSEData(w, first)
		flush(fl)
	}
	ctx, cancel := context.WithTimeout(r.Context(), runTimeout)
	defer cancel()
	lim := newLimiter(turn.Stop, turn.MaxTokens)
	var streamed bool
	start := time.Now()
	res, err := s.execute(ctx, turn, func(d Delta) error {
		if d.Reasoning != "" {
			payload, err := openaiDelta(id, turn.Model, map[string]any{"reasoning_content": d.Reasoning}, nil, nil)
			if err != nil {
				return err
			}
			if err := WriteSSEData(w, payload); err != nil {
				return err
			}
			flush(fl)
		}
		if d.Text != "" {
			piece, done := lim.Push(d.Text)
			if piece != "" {
				streamed = true
				payload, err := openaiChunkJSON(id, turn.Model, "", piece, nil)
				if err != nil {
					return err
				}
				if err := WriteSSEData(w, payload); err != nil {
					return err
				}
				flush(fl)
			}
			if done {
				cancel()
			}
		}
		return nil
	})
	if tail := lim.Flush(); tail != "" {
		streamed = true
		if payload, err := openaiChunkJSON(id, turn.Model, "", tail, nil); err == nil {
			_ = WriteSSEData(w, payload)
			flush(fl)
		}
	}
	if err != nil && !lim.done {
		s.note("/v1/chat/completions", turn.Model, false, err.Error(), time.Since(start), Usage{})
		errPayload, _ := json.Marshal(map[string]any{"error": map[string]any{"message": err.Error(), "type": "upstream_error"}})
		_ = WriteSSEData(w, string(errPayload))
		_ = WriteSSEDone(w)
		flush(fl)
		return
	}
	if lim.done {
		res.Finish = lim.finish
		res.StopMatch = lim.matched
		res.Text = lim.Text()
		err = nil
	}
	if !streamed && res.Text != "" && !lim.done {
		writeLimitedText(w, fl, id, turn.Model, res.Text, lim)
		res.Text = lim.Text()
		if lim.done {
			res.Finish = lim.finish
			res.StopMatch = lim.matched
		}
	}
	for i, tc := range res.Tools {
		payload, err := openaiDelta(id, turn.Model, map[string]any{
			"tool_calls": []map[string]any{{
				"index": i,
				"id":    tc.ID,
				"type":  "function",
				"function": map[string]any{
					"name":      tc.Name,
					"arguments": tc.Arguments,
				},
			}},
		}, nil, nil)
		if err == nil {
			_ = WriteSSEData(w, payload)
			flush(fl)
		}
	}
	finish := openAIFinish(res.Finish)
	var usage any
	if includeUsage {
		usage = usageMap(res.Usage)
	}
	if last, err := openaiDelta(id, turn.Model, map[string]any{}, &finish, usage); err == nil {
		_ = WriteSSEData(w, last)
	}
	_ = WriteSSEDone(w)
	flush(fl)
	s.note("/v1/chat/completions", turn.Model, true, "", time.Since(start), res.Usage)
}

func writeLimitedText(w http.ResponseWriter, fl http.Flusher, id, model, text string, lim *limiter) {
	for _, part := range chunkRunes(text, 48) {
		piece, done := lim.Push(part)
		if piece != "" {
			if payload, err := openaiChunkJSON(id, model, "", piece, nil); err == nil {
				_ = WriteSSEData(w, payload)
				flush(fl)
			}
		}
		if done {
			return
		}
	}
	if tail := lim.Flush(); tail != "" {
		if payload, err := openaiChunkJSON(id, model, "", tail, nil); err == nil {
			_ = WriteSSEData(w, payload)
			flush(fl)
		}
	}
}

func (s *Server) finishAnthropic(w http.ResponseWriter, r *http.Request, turn Turn) {
	ctx, cancel := context.WithTimeout(r.Context(), runTimeout)
	defer cancel()
	start := time.Now()
	res, err := s.execute(ctx, turn, nil)
	if err != nil {
		s.note("/v1/messages", turn.Model, false, err.Error(), time.Since(start), Usage{})
		writeHTTPError(w, true, err)
		return
	}
	res = limitResult(res, turn)
	s.note("/v1/messages", turn.Model, true, "", time.Since(start), res.Usage)
	reason, seq := anthropicStop(res.Finish, res.StopMatch)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":            newID("msg"),
		"type":          "message",
		"role":          "assistant",
		"model":         turn.Model,
		"content":       anthropicContent(res.Text, res.Reasoning, res.Tools),
		"stop_reason":   reason,
		"stop_sequence": seq,
		"usage": map[string]int{
			"input_tokens":  res.Usage.Input,
			"output_tokens": res.Usage.Completion(),
		},
	})
}

func (s *Server) streamAnthropic(w http.ResponseWriter, r *http.Request, turn Turn) {
	if strings.TrimSpace(turn.Model) == "" {
		writeHTTPError(w, true, badRequest("model", "model is required"))
		return
	}
	fl := flushOf(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flush(fl)

	id := newID("msg")
	_ = WriteSSEEvent(w, "message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id": id, "type": "message", "role": "assistant", "model": turn.Model,
			"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
			"usage": map[string]int{"input_tokens": 0, "output_tokens": 0},
		},
	})
	flush(fl)

	ctx, cancel := context.WithTimeout(r.Context(), runTimeout)
	defer cancel()
	st := &anthStream{w: w, fl: fl}
	lim := newLimiter(turn.Stop, turn.MaxTokens)
	start := time.Now()
	var streamed bool
	res, err := s.execute(ctx, turn, func(d Delta) error {
		if d.Reasoning != "" {
			if err := st.reasoning(d.Reasoning); err != nil {
				return err
			}
		}
		if d.Text != "" {
			piece, done := lim.Push(d.Text)
			if piece != "" {
				streamed = true
				if err := st.text(piece); err != nil {
					return err
				}
			}
			if done {
				cancel()
			}
		}
		return nil
	})
	if tail := lim.Flush(); tail != "" {
		streamed = true
		_ = st.text(tail)
	}
	if err != nil && !lim.done {
		s.note("/v1/messages", turn.Model, false, err.Error(), time.Since(start), Usage{})
		_ = WriteSSEEvent(w, "error", map[string]any{
			"type":  "error",
			"error": map[string]any{"type": "upstream_error", "message": err.Error()},
		})
		_ = WriteSSEEvent(w, "message_stop", map[string]any{"type": "message_stop"})
		flush(fl)
		return
	}
	if lim.done {
		res.Finish = lim.finish
		res.StopMatch = lim.matched
		err = nil
	}
	if !streamed && res.Text != "" && !lim.done {
		for _, part := range chunkRunes(res.Text, 48) {
			piece, done := lim.Push(part)
			if piece != "" {
				_ = st.text(piece)
			}
			if done {
				res.Finish = lim.finish
				res.StopMatch = lim.matched
				break
			}
		}
		if tail := lim.Flush(); tail != "" {
			_ = st.text(tail)
		}
		if lim.done {
			res.Finish = lim.finish
			res.StopMatch = lim.matched
		}
	}
	for _, tc := range res.Tools {
		_ = st.tool(tc)
	}
	st.close()
	reason, seq := anthropicStop(res.Finish, res.StopMatch)
	_ = WriteSSEEvent(w, "message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": reason, "stop_sequence": seq},
		"usage": map[string]int{"output_tokens": res.Usage.Completion()},
	})
	_ = WriteSSEEvent(w, "message_stop", map[string]any{"type": "message_stop"})
	flush(fl)
	s.note("/v1/messages", turn.Model, true, "", time.Since(start), res.Usage)
}

type anthStream struct {
	w        io.Writer
	fl       http.Flusher
	index    int
	open     bool
	kind     string
	toolOpen bool
}

func (a *anthStream) start(kind string, block map[string]any) error {
	if a.open {
		if err := a.stop(); err != nil {
			return err
		}
	}
	block["type"] = kind
	err := WriteSSEEvent(a.w, "content_block_start", map[string]any{
		"type":          "content_block_start",
		"index":         a.index,
		"content_block": block,
	})
	if err == nil {
		a.open = true
		a.kind = kind
		flush(a.fl)
	}
	return err
}

func (a *anthStream) stop() error {
	if !a.open {
		return nil
	}
	err := WriteSSEEvent(a.w, "content_block_stop", map[string]any{
		"type":  "content_block_stop",
		"index": a.index,
	})
	a.open = false
	a.index++
	flush(a.fl)
	return err
}

func (a *anthStream) close() { _ = a.stop() }

func (a *anthStream) text(s string) error {
	if !a.open || a.kind != "text" {
		if err := a.start("text", map[string]any{"text": ""}); err != nil {
			return err
		}
	}
	err := WriteSSEEvent(a.w, "content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": a.index,
		"delta": map[string]any{"type": "text_delta", "text": s},
	})
	flush(a.fl)
	return err
}

func (a *anthStream) reasoning(s string) error {
	if !a.open || a.kind != "thinking" {
		if err := a.start("thinking", map[string]any{"thinking": ""}); err != nil {
			return err
		}
	}
	err := WriteSSEEvent(a.w, "content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": a.index,
		"delta": map[string]any{"type": "thinking_delta", "thinking": s},
	})
	flush(a.fl)
	return err
}

func (a *anthStream) tool(tc ToolCall) error {
	var input any
	if err := json.Unmarshal([]byte(tc.Arguments), &input); err != nil {
		input = map[string]any{}
	}
	if err := a.start("tool_use", map[string]any{"id": tc.ID, "name": tc.Name, "input": map[string]any{}}); err != nil {
		return err
	}
	partial, _ := json.Marshal(input)
	err := WriteSSEEvent(a.w, "content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": a.index,
		"delta": map[string]any{"type": "input_json_delta", "partial_json": string(partial)},
	})
	flush(a.fl)
	return err
}

func (s *Server) finishResponses(w http.ResponseWriter, r *http.Request, turn Turn) {
	ctx, cancel := context.WithTimeout(r.Context(), runTimeout)
	defer cancel()
	start := time.Now()
	res, err := s.execute(ctx, turn, nil)
	if err != nil {
		s.note("/v1/responses", turn.Model, false, err.Error(), time.Since(start), Usage{})
		writeHTTPError(w, false, err)
		return
	}
	res = limitResult(res, turn)
	s.note("/v1/responses", turn.Model, true, "", time.Since(start), res.Usage)
	id := newID("resp")
	s.remember(id, res.SessionID)
	writeJSON(w, http.StatusOK, responseObject(id, turn.Model, res))
}

func (s *Server) streamResponses(w http.ResponseWriter, r *http.Request, turn Turn) {
	if strings.TrimSpace(turn.Model) == "" {
		writeHTTPError(w, false, badRequest("model", "model is required"))
		return
	}
	fl := flushOf(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flush(fl)

	id := newID("resp")
	ctx, cancel := context.WithTimeout(r.Context(), runTimeout)
	defer cancel()
	created := responseObject(id, turn.Model, RunResult{Finish: "stop"})
	created["status"] = "in_progress"
	created["output"] = []any{}
	_ = WriteSSEEvent(w, "response.created", map[string]any{"type": "response.created", "response": created})
	flush(fl)

	lim := newLimiter(turn.Stop, turn.MaxTokens)
	msgID := newID("msg")
	var opened bool
	var streamed bool
	start := time.Now()
	ensure := func() {
		if opened {
			return
		}
		opened = true
		item := map[string]any{
			"type": "message", "id": msgID, "role": "assistant", "status": "in_progress",
			"content": []any{},
		}
		_ = WriteSSEEvent(w, "response.output_item.added", map[string]any{
			"type": "response.output_item.added", "output_index": 0, "item": item,
		})
		_ = WriteSSEEvent(w, "response.content_part.added", map[string]any{
			"type": "response.content_part.added", "output_index": 0, "content_index": 0,
			"part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}},
		})
		flush(fl)
	}
	writeText := func(piece string) {
		if piece == "" {
			return
		}
		ensure()
		streamed = true
		_ = WriteSSEEvent(w, "response.output_text.delta", map[string]any{
			"type": "response.output_text.delta", "output_index": 0, "content_index": 0, "delta": piece,
		})
		flush(fl)
	}
	res, err := s.execute(ctx, turn, func(d Delta) error {
		if d.Text == "" {
			return nil
		}
		piece, done := lim.Push(d.Text)
		writeText(piece)
		if done {
			cancel()
		}
		return nil
	})
	if tail := lim.Flush(); tail != "" {
		writeText(tail)
	}
	if err != nil && !lim.done {
		s.note("/v1/responses", turn.Model, false, err.Error(), time.Since(start), Usage{})
		_ = WriteSSEEvent(w, "error", map[string]any{"type": "error", "error": map[string]any{"message": err.Error(), "type": "upstream_error"}})
		flush(fl)
		return
	}
	if lim.done {
		res.Finish = lim.finish
		res.Text = lim.Text()
	}
	if !streamed && res.Text != "" && !lim.done {
		for _, part := range chunkRunes(res.Text, 48) {
			piece, done := lim.Push(part)
			writeText(piece)
			if done {
				break
			}
		}
		writeText(lim.Flush())
		res.Text = lim.Text()
		if lim.done {
			res.Finish = lim.finish
		}
	}
	if opened {
		_ = WriteSSEEvent(w, "response.output_text.done", map[string]any{
			"type": "response.output_text.done", "output_index": 0, "content_index": 0, "text": res.Text,
		})
		_ = WriteSSEEvent(w, "response.content_part.done", map[string]any{
			"type": "response.content_part.done", "output_index": 0, "content_index": 0,
			"part": map[string]any{"type": "output_text", "text": res.Text, "annotations": []any{}},
		})
		_ = WriteSSEEvent(w, "response.output_item.done", map[string]any{
			"type": "response.output_item.done", "output_index": 0,
			"item": map[string]any{"type": "message", "id": msgID, "role": "assistant", "status": "completed", "content": []map[string]any{{"type": "output_text", "text": res.Text, "annotations": []any{}}}},
		})
	}
	for i, tc := range res.Tools {
		item := map[string]any{
			"type": "function_call", "id": newID("fc"), "call_id": tc.ID, "name": tc.Name, "arguments": tc.Arguments, "status": "completed",
		}
		_ = WriteSSEEvent(w, "response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": i + 1, "item": item})
		_ = WriteSSEEvent(w, "response.function_call_arguments.done", map[string]any{
			"type": "response.function_call_arguments.done", "output_index": i + 1, "arguments": tc.Arguments,
		})
		_ = WriteSSEEvent(w, "response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": i + 1, "item": item})
	}
	final := responseObject(id, turn.Model, res)
	_ = WriteSSEEvent(w, "response.completed", map[string]any{"type": "response.completed", "response": final})
	flush(fl)
	s.remember(id, res.SessionID)
	s.note("/v1/responses", turn.Model, true, "", time.Since(start), res.Usage)
}

func responseObject(id, model string, res RunResult) map[string]any {
	status := "completed"
	if res.Finish == "length" {
		status = "incomplete"
	}
	return map[string]any{
		"id":         id,
		"object":     "response",
		"created_at": createdUnix(),
		"status":     status,
		"model":      model,
		"output":     responseOutput(res.Text, res.Reasoning, res.Tools),
		"usage": map[string]any{
			"input_tokens":  res.Usage.Input,
			"output_tokens": res.Usage.Completion(),
			"total_tokens":  res.Usage.Total(),
		},
	}
}

func limitResult(res RunResult, turn Turn) RunResult {
	if res.Finish == "tool_calls" && len(res.Tools) > 0 {
		return res
	}
	out, finish, matched := applyLimits(res.Text, turn.Stop, turn.MaxTokens)
	res.Text = out
	if finish != "" && (res.Finish == "" || res.Finish == "stop") {
		res.Finish = finish
		res.StopMatch = matched
	}
	return res
}

func (s *Server) execute(ctx context.Context, turn Turn, emit func(Delta) error) (RunResult, error) {
	if strings.TrimSpace(turn.Model) == "" {
		return RunResult{}, badRequest("model", "model is required")
	}
	turn.Model = s.resolveModel(turn.Model)
	provider, modelID := splitModel(turn.Model)
	files, err := materializeFiles(s.workDir, turn.Files)
	if err != nil {
		return RunResult{}, err
	}
	schema := toolSchema(turn.Tools, turn.ToolChoice)
	system := turn.System
	if schema != nil {
		system = withToolInstructions(system, turn.Tools, turn.ToolChoice)
	}
	if turn.MaxTokens > 0 {
		system = appendHint(system, "Keep the visible reply within the requested token limit.")
	}
	prompt := strings.TrimSpace(turn.Transcript)
	if prompt == "" && len(files) == 0 {
		return RunResult{}, badRequest("messages", "messages produced an empty prompt")
	}
	if prompt == "" {
		prompt = "Review the attached files."
	}
	res, err := s.engine.Run(ctx, RunRequest{
		ProviderID:  provider,
		ModelID:     modelID,
		System:      system,
		Prompt:      prompt,
		Files:       files,
		Variant:     turn.Variant,
		ClientTools: schema != nil,
		Schema:      schema,
		SessionID:   turn.SessionID,
	}, emit)
	if err != nil {
		return res, err
	}
	if res.Usage.Input == 0 {
		res.Usage.Input = EstimateTokens(system) + EstimateTokens(prompt)
	}
	if res.Usage.Output == 0 && res.Usage.Reasoning == 0 {
		res.Usage.Output = EstimateTokens(res.Text)
		res.Usage.Reasoning = EstimateTokens(res.Reasoning)
	}
	if res.Finish == "" {
		if len(res.Tools) > 0 {
			res.Finish = "tool_calls"
		} else {
			res.Finish = "stop"
		}
	}
	return res, nil
}

func chunkRunes(s string, n int) []string {
	if s == "" {
		return nil
	}
	r := []rune(s)
	var out []string
	for len(r) > 0 {
		if len(r) <= n {
			out = append(out, string(r))
			break
		}
		out = append(out, string(r[:n]))
		r = r[n:]
	}
	return out
}

func openaiDelta(id, model string, delta map[string]any, finish *string, usage any) (string, error) {
	if delta == nil {
		delta = map[string]any{}
	}
	chunk := map[string]any{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": createdUnix(),
		"model":   model,
		"choices": []map[string]any{{
			"index":         0,
			"delta":         delta,
			"finish_reason": finish,
		}},
	}
	if usage != nil {
		chunk["usage"] = usage
	}
	b, err := json.Marshal(chunk)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	defer r.Body.Close()
	buf, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, badRequest("", "invalid JSON body")
	}
	return buf, nil
}

func writeHTTPError(w http.ResponseWriter, anthropic bool, err error) {
	var he *httpError
	if errors.As(err, &he) {
		if anthropic {
			writeAnthropicError(w, he.Status, orType(he.Type), he.Msg)
			return
		}
		writeOpenAIError(w, he.Status, orType(he.Type), he.Param, he.Msg)
		return
	}
	status := http.StatusBadGateway
	if errors.Is(err, context.DeadlineExceeded) {
		status = http.StatusGatewayTimeout
	}
	msg := err.Error()
	if anthropic {
		writeAnthropicError(w, status, "upstream_error", msg)
		return
	}
	writeOpenAIError(w, status, "upstream_error", "", msg)
}

func orType(t string) string {
	if t == "" {
		return "invalid_request_error"
	}
	return t
}

func writeOpenAIError(w http.ResponseWriter, status int, typ, param, msg string) {
	errObj := map[string]any{"message": msg, "type": typ}
	if param != "" {
		errObj["param"] = param
	}
	writeJSON(w, status, map[string]any{"error": errObj})
}

func writeAnthropicError(w http.ResponseWriter, status int, typ, msg string) {
	writeJSON(w, status, map[string]any{
		"type": "error",
		"error": map[string]any{
			"type":    typ,
			"message": msg,
		},
	})
}
