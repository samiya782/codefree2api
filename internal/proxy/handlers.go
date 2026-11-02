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
	s.finishTurn(w, r, "/v1/chat/completions", false, turn, func(res RunResult) {
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
	})
}

func (s *Server) streamOpenAI(w http.ResponseWriter, r *http.Request, turn Turn, includeUsage bool) {
	s.streamTurn(w, r, "/v1/chat/completions", false, turn, func(fl http.Flusher) turnSink {
		id := newID("chatcmpl")
		if first, err := openaiChunkJSON(id, turn.Model, "assistant", "", nil); err == nil {
			_ = WriteSSEData(w, first)
			flush(fl)
		}
		return &openAIStream{w: w, fl: fl, id: id, model: turn.Model, includeUsage: includeUsage}
	})
}

func (s *Server) finishAnthropic(w http.ResponseWriter, r *http.Request, turn Turn) {
	s.finishTurn(w, r, "/v1/messages", true, turn, func(res RunResult) {
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
	})
}

func (s *Server) streamAnthropic(w http.ResponseWriter, r *http.Request, turn Turn) {
	s.streamTurn(w, r, "/v1/messages", true, turn, func(fl http.Flusher) turnSink {
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
		return &anthropicStream{w: w, fl: fl, blocks: anthStream{w: w, fl: fl}}
	})
}

func (s *Server) finishResponses(w http.ResponseWriter, r *http.Request, turn Turn) {
	s.finishTurn(w, r, "/v1/responses", false, turn, func(res RunResult) {
		id := newID("resp")
		s.remember(id, res.SessionID)
		writeJSON(w, http.StatusOK, responseObject(id, turn.Model, res))
	})
}

func (s *Server) streamResponses(w http.ResponseWriter, r *http.Request, turn Turn) {
	s.streamTurn(w, r, "/v1/responses", false, turn, func(fl http.Flusher) turnSink {
		id := newID("resp")
		created := responseObject(id, turn.Model, RunResult{Finish: "stop"})
		created["status"] = "in_progress"
		created["output"] = []any{}
		_ = WriteSSEEvent(w, "response.created", map[string]any{"type": "response.created", "response": created})
		flush(fl)
		return &responsesStream{w: w, fl: fl, srv: s, id: id, model: turn.Model}
	})
}

func (s *Server) finishTurn(w http.ResponseWriter, r *http.Request, endpoint string, anthropic bool, turn Turn, write func(RunResult)) {
	ctx, cancel := context.WithTimeout(r.Context(), runTimeout)
	defer cancel()
	start := time.Now()
	res, err := s.execute(ctx, turn, nil)
	if err != nil {
		s.note(endpoint, turn.Model, false, err.Error(), time.Since(start), Usage{})
		writeHTTPError(w, anthropic, err)
		return
	}
	res = limitResult(res, turn)
	s.note(endpoint, turn.Model, true, "", time.Since(start), res.Usage)
	write(res)
}

type turnSink interface {
	reasoning(string) error
	text(string) error
	tools([]ToolCall) error
	finish(RunResult) error
	fail(error)
}

func (s *Server) streamTurn(w http.ResponseWriter, r *http.Request, endpoint string, anthropic bool, turn Turn, prepare func(http.Flusher) turnSink) {
	if strings.TrimSpace(turn.Model) == "" {
		writeHTTPError(w, anthropic, badRequest("model", "model is required"))
		return
	}
	fl := flushOf(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flush(fl)
	sink := prepare(fl)

	ctx, cancel := context.WithTimeout(r.Context(), runTimeout)
	defer cancel()
	lim := newLimiter(turn.Stop, turn.MaxTokens)
	var streamed bool
	start := time.Now()
	res, err := s.execute(ctx, turn, func(d Delta) error {
		if d.Reasoning != "" {
			if err := sink.reasoning(d.Reasoning); err != nil {
				return err
			}
		}
		if d.Text != "" {
			piece, done := lim.Push(d.Text)
			if piece != "" {
				streamed = true
				if err := sink.text(piece); err != nil {
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
		_ = sink.text(tail)
	}
	if err != nil && !lim.done {
		s.note(endpoint, turn.Model, false, err.Error(), time.Since(start), Usage{})
		sink.fail(err)
		return
	}
	if lim.done {
		res.Finish = lim.finish
		res.StopMatch = lim.matched
		res.Text = lim.Text()
	}
	if !streamed && res.Text != "" && !lim.done {
		for _, part := range chunkRunes(res.Text, 48) {
			piece, done := lim.Push(part)
			if piece != "" {
				_ = sink.text(piece)
			}
			if done {
				break
			}
		}
		if tail := lim.Flush(); tail != "" {
			_ = sink.text(tail)
		}
		res.Text = lim.Text()
		if lim.done {
			res.Finish = lim.finish
			res.StopMatch = lim.matched
		}
	}
	_ = sink.tools(res.Tools)
	_ = sink.finish(res)
	s.note(endpoint, turn.Model, true, "", time.Since(start), res.Usage)
}

type openAIStream struct {
	w            http.ResponseWriter
	fl           http.Flusher
	id           string
	model        string
	includeUsage bool
}

func (o *openAIStream) reasoning(s string) error {
	return o.delta(map[string]any{"reasoning_content": s}, nil, nil)
}

func (o *openAIStream) text(s string) error {
	payload, err := openaiChunkJSON(o.id, o.model, "", s, nil)
	if err != nil {
		return err
	}
	if err := WriteSSEData(o.w, payload); err != nil {
		return err
	}
	flush(o.fl)
	return nil
}

func (o *openAIStream) tools(calls []ToolCall) error {
	for i, tc := range calls {
		_ = o.delta(map[string]any{
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
	}
	return nil
}

func (o *openAIStream) finish(res RunResult) error {
	finish := openAIFinish(res.Finish)
	var usage any
	if o.includeUsage {
		usage = usageMap(res.Usage)
	}
	_ = o.delta(map[string]any{}, &finish, usage)
	_ = WriteSSEDone(o.w)
	flush(o.fl)
	return nil
}

func (o *openAIStream) fail(err error) {
	errPayload, _ := json.Marshal(map[string]any{"error": map[string]any{"message": err.Error(), "type": "upstream_error"}})
	_ = WriteSSEData(o.w, string(errPayload))
	_ = WriteSSEDone(o.w)
	flush(o.fl)
}

func (o *openAIStream) delta(delta map[string]any, finish *string, usage any) error {
	payload, err := openaiDelta(o.id, o.model, delta, finish, usage)
	if err != nil {
		return err
	}
	if err := WriteSSEData(o.w, payload); err != nil {
		return err
	}
	flush(o.fl)
	return nil
}

type anthStream struct {
	w     io.Writer
	fl    http.Flusher
	index int
	open  bool
	kind  string
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

type anthropicStream struct {
	w      http.ResponseWriter
	fl     http.Flusher
	blocks anthStream
}

func (a *anthropicStream) reasoning(s string) error { return a.blocks.reasoning(s) }
func (a *anthropicStream) text(s string) error      { return a.blocks.text(s) }
func (a *anthropicStream) tools(calls []ToolCall) error {
	for _, tc := range calls {
		if err := a.blocks.tool(tc); err != nil {
			return err
		}
	}
	return nil
}

func (a *anthropicStream) finish(res RunResult) error {
	a.blocks.close()
	reason, seq := anthropicStop(res.Finish, res.StopMatch)
	_ = WriteSSEEvent(a.w, "message_delta", map[string]any{
		"type":  "message_delta",
		"delta": map[string]any{"stop_reason": reason, "stop_sequence": seq},
		"usage": map[string]int{"output_tokens": res.Usage.Completion()},
	})
	_ = WriteSSEEvent(a.w, "message_stop", map[string]any{"type": "message_stop"})
	flush(a.fl)
	return nil
}

func (a *anthropicStream) fail(err error) {
	_ = WriteSSEEvent(a.w, "error", map[string]any{
		"type":  "error",
		"error": map[string]any{"type": "upstream_error", "message": err.Error()},
	})
	_ = WriteSSEEvent(a.w, "message_stop", map[string]any{"type": "message_stop"})
	flush(a.fl)
}

type responsesStream struct {
	w            http.ResponseWriter
	fl           http.Flusher
	srv          *Server
	id           string
	model        string
	msgID        string
	next         int
	msgIndex     int
	reasonIndex  int
	reasonID     string
	msgOpen      bool
	reasonOpen   bool
	reasonClosed bool
}

func (e *responsesStream) reasoning(s string) error {
	if !e.reasonOpen {
		e.reasonOpen = true
		e.reasonIndex = e.next
		e.reasonID = newID("rs")
		e.next++
		_ = e.event("response.output_item.added", map[string]any{
			"type": "response.output_item.added", "output_index": e.reasonIndex,
			"item": map[string]any{"type": "reasoning", "id": e.reasonID, "summary": []any{}},
		})
	}
	return e.event("response.reasoning_summary_text.delta", map[string]any{
		"type": "response.reasoning_summary_text.delta", "output_index": e.reasonIndex, "delta": s,
	})
}

func (e *responsesStream) text(piece string) error {
	if piece == "" {
		return nil
	}
	if !e.msgOpen {
		e.msgOpen = true
		e.msgIndex = e.next
		e.next++
		e.msgID = newID("msg")
		_ = e.event("response.output_item.added", map[string]any{
			"type": "response.output_item.added", "output_index": e.msgIndex,
			"item": map[string]any{
				"type": "message", "id": e.msgID, "role": "assistant", "status": "in_progress",
				"content": []any{},
			},
		})
		_ = e.event("response.content_part.added", map[string]any{
			"type": "response.content_part.added", "output_index": e.msgIndex, "content_index": 0,
			"part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}},
		})
	}
	return e.event("response.output_text.delta", map[string]any{
		"type": "response.output_text.delta", "output_index": e.msgIndex, "content_index": 0, "delta": piece,
	})
}

func (e *responsesStream) tools(calls []ToolCall) error {
	base := e.next
	if base == 0 {
		base = 1
	}
	for i, tc := range calls {
		item := map[string]any{
			"type": "function_call", "id": newID("fc"), "call_id": tc.ID, "name": tc.Name, "arguments": tc.Arguments, "status": "completed",
		}
		_ = e.event("response.output_item.added", map[string]any{"type": "response.output_item.added", "output_index": base + i, "item": item})
		_ = e.event("response.function_call_arguments.done", map[string]any{
			"type": "response.function_call_arguments.done", "output_index": base + i, "arguments": tc.Arguments,
		})
		_ = e.event("response.output_item.done", map[string]any{"type": "response.output_item.done", "output_index": base + i, "item": item})
	}
	return nil
}

func (e *responsesStream) finish(res RunResult) error {
	e.closeReason(res.Reasoning)
	if e.msgOpen {
		_ = e.event("response.output_text.done", map[string]any{
			"type": "response.output_text.done", "output_index": e.msgIndex, "content_index": 0, "text": res.Text,
		})
		_ = e.event("response.content_part.done", map[string]any{
			"type": "response.content_part.done", "output_index": e.msgIndex, "content_index": 0,
			"part": map[string]any{"type": "output_text", "text": res.Text, "annotations": []any{}},
		})
		_ = e.event("response.output_item.done", map[string]any{
			"type": "response.output_item.done", "output_index": e.msgIndex,
			"item": map[string]any{
				"type": "message", "id": e.msgID, "role": "assistant", "status": "completed",
				"content": []map[string]any{{"type": "output_text", "text": res.Text, "annotations": []any{}}},
			},
		})
	}
	_ = e.event("response.completed", map[string]any{"type": "response.completed", "response": responseObject(e.id, e.model, res)})
	e.srv.remember(e.id, res.SessionID)
	return nil
}

func (e *responsesStream) fail(err error) {
	_ = e.event("error", map[string]any{"type": "error", "error": map[string]any{"message": err.Error(), "type": "upstream_error"}})
}

func (e *responsesStream) closeReason(text string) {
	if !e.reasonOpen || e.reasonClosed {
		return
	}
	e.reasonClosed = true
	_ = e.event("response.output_item.done", map[string]any{
		"type": "response.output_item.done", "output_index": e.reasonIndex,
		"item": map[string]any{
			"type": "reasoning", "id": e.reasonID,
			"summary": []map[string]any{{"type": "summary_text", "text": text}},
		},
	})
}

func (e *responsesStream) event(name string, payload map[string]any) error {
	err := WriteSSEEvent(e.w, name, payload)
	flush(e.fl)
	return err
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
