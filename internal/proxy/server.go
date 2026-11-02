package proxy

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const maxBody = 10 << 20

type Options struct {
	Port     int
	Password string
	Bin      string
}

type LastCall struct {
	At       time.Time
	Endpoint string
	Model    string
	OK       bool
	Error    string
	Duration time.Duration
}

type Server struct {
	addr     string
	password string
	bin      string
	runner   *Runner
	mux      *http.ServeMux

	mu         sync.Mutex
	last       LastCall
	modelCache []string
}

func New(opt Options) (*Server, error) {
	if opt.Port <= 0 {
		opt.Port = DefaultPort
	}
	if opt.Bin == "" {
		opt.Bin = DefaultBin
	}
	workDir := filepath.Join(os.TempDir(), "codefree2api-work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, fmt.Errorf("work dir: %w", err)
	}
	s := &Server{
		addr:     net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", opt.Port)),
		password: opt.Password,
		bin:      opt.Bin,
		runner:   &Runner{Bin: opt.Bin, WorkDir: workDir},
		mux:      http.NewServeMux(),
	}
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /v1/models", s.handleModels)
	s.mux.HandleFunc("POST /v1/chat/completions", s.handleChat)
	s.mux.HandleFunc("POST /v1/messages", s.handleMessages)
	s.mux.HandleFunc("GET /admin", s.handleAdmin)
	return s, nil
}

func (s *Server) Addr() string { return s.addr }
func (s *Server) Bin() string  { return s.bin }

func (s *Server) HTTPServer() *http.Server {
	return &http.Server{
		Addr:              s.addr,
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/health" && !s.authorized(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="codefree2api"`)
		writeJSONError(w, http.StatusUnauthorized, "auth_error", "unauthorized: missing or invalid Bearer token")
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) authorized(r *http.Request) bool {
	if s.password == "" {
		return true
	}
	h := r.Header.Get("Authorization")
	scheme, token, ok := strings.Cut(h, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return false
	}
	token = strings.TrimSpace(token)
	return secureEqual(token, s.password)
}

func secureEqual(a, b string) bool {
	ab := []byte(a)
	bb := []byte(b)
	if len(ab) != len(bb) {
		subtle.ConstantTimeCompare(bb, bb)
		return false
	}
	return subtle.ConstantTimeCompare(ab, bb) == 1
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), listTimeout)
	defer cancel()
	start := time.Now()
	models, err := s.runner.ListModels(ctx)
	if err != nil {
		s.note("/v1/models", "", false, err.Error(), time.Since(start))
		writeJSONError(w, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}
	s.mu.Lock()
	s.modelCache = append([]string(nil), models...)
	s.mu.Unlock()
	s.note("/v1/models", "", true, "", time.Since(start))

	created := createdUnix()
	data := make([]map[string]any, 0, len(models))
	for _, id := range models {
		data = append(data, map[string]any{
			"id":       id,
			"object":   "model",
			"created":  created,
			"owned_by": "codefree",
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

type chatRequest struct {
	Model    string            `json:"model"`
	Messages []IncomingMessage `json:"messages"`
	Stream   bool              `json:"stream"`
}

type anthropicRequest struct {
	Model     string            `json:"model"`
	Messages  []IncomingMessage `json:"messages"`
	System    json.RawMessage   `json:"system"`
	Stream    bool              `json:"stream"`
	MaxTokens int               `json:"max_tokens"`
}

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	if strings.TrimSpace(req.Model) == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	prompt := MessagesToPrompt(req.Messages, "")
	if prompt == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid_request_error", "messages produced an empty prompt")
		return
	}
	if req.Stream {
		s.streamChat(w, r, req.Model, prompt)
		return
	}
	s.completeChat(w, r, req.Model, prompt)
}

func (s *Server) completeChat(w http.ResponseWriter, r *http.Request, model, prompt string) {
	ctx, cancel := context.WithTimeout(r.Context(), runTimeout)
	defer cancel()
	start := time.Now()
	out, err := s.runner.Run(ctx, model, prompt, nil)
	dur := time.Since(start)
	if err != nil {
		s.note("/v1/chat/completions", model, false, err.Error(), dur)
		status := http.StatusBadGateway
		if ctx.Err() != nil {
			status = http.StatusGatewayTimeout
		}
		writeJSONError(w, status, "upstream_error", err.Error())
		return
	}
	s.note("/v1/chat/completions", model, true, "", dur)
	id := newID("chatcmpl")
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"created": createdUnix(),
		"model":   model,
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": out,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"total_tokens":      0,
		},
	})
}

func (s *Server) streamChat(w http.ResponseWriter, r *http.Request, model, prompt string) {
	fl := flushOf(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flush(fl)

	id := newID("chatcmpl")
	first, err := openaiChunkJSON(id, model, "assistant", "", nil)
	if err == nil {
		_ = WriteSSEData(w, first)
		flush(fl)
	}

	ctx, cancel := context.WithTimeout(r.Context(), runTimeout)
	defer cancel()
	start := time.Now()
	_, runErr := s.runner.Run(ctx, model, prompt, func(chunk string) error {
		payload, err := openaiChunkJSON(id, model, "", chunk, nil)
		if err != nil {
			return err
		}
		if err := WriteSSEData(w, payload); err != nil {
			return err
		}
		flush(fl)
		return nil
	})
	dur := time.Since(start)
	if runErr != nil {
		s.note("/v1/chat/completions", model, false, runErr.Error(), dur)
		errPayload, _ := json.Marshal(map[string]any{
			"error": map[string]any{"message": runErr.Error(), "type": "upstream_error"},
		})
		_ = WriteSSEData(w, string(errPayload))
		_ = WriteSSEDone(w)
		flush(fl)
		return
	}
	s.note("/v1/chat/completions", model, true, "", dur)
	stop := "stop"
	last, err := openaiChunkJSON(id, model, "", "", &stop)
	if err == nil {
		_ = WriteSSEData(w, last)
	}
	_ = WriteSSEDone(w)
	flush(fl)
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	var req anthropicRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	if strings.TrimSpace(req.Model) == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	system := ExtractContent(req.System)
	prompt := MessagesToPrompt(req.Messages, system)
	if prompt == "" {
		writeJSONError(w, http.StatusBadRequest, "invalid_request_error", "messages produced an empty prompt")
		return
	}
	if req.Stream {
		s.streamAnthropic(w, r, req.Model, prompt)
		return
	}
	s.completeAnthropic(w, r, req.Model, prompt)
}

func (s *Server) completeAnthropic(w http.ResponseWriter, r *http.Request, model, prompt string) {
	ctx, cancel := context.WithTimeout(r.Context(), runTimeout)
	defer cancel()
	start := time.Now()
	out, err := s.runner.Run(ctx, model, prompt, nil)
	dur := time.Since(start)
	if err != nil {
		s.note("/v1/messages", model, false, err.Error(), dur)
		status := http.StatusBadGateway
		if ctx.Err() != nil {
			status = http.StatusGatewayTimeout
		}
		writeJSONError(w, status, "upstream_error", err.Error())
		return
	}
	s.note("/v1/messages", model, true, "", dur)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":    newID("msg"),
		"type":  "message",
		"role":  "assistant",
		"model": model,
		"content": []map[string]any{
			{"type": "text", "text": out},
		},
		"stop_reason":   "end_turn",
		"stop_sequence": nil,
		"usage": map[string]int{
			"input_tokens":  0,
			"output_tokens": 0,
		},
	})
}

func (s *Server) streamAnthropic(w http.ResponseWriter, r *http.Request, model, prompt string) {
	fl := flushOf(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flush(fl)

	id := newID("msg")
	msgStart := map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            id,
			"type":          "message",
			"role":          "assistant",
			"model":         model,
			"content":       []any{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         map[string]int{"input_tokens": 0, "output_tokens": 0},
		},
	}
	_ = WriteSSEEvent(w, "message_start", msgStart)
	flush(fl)

	ctx, cancel := context.WithTimeout(r.Context(), runTimeout)
	defer cancel()
	start := time.Now()
	_, runErr := s.runner.Run(ctx, model, prompt, func(chunk string) error {
		delta := map[string]any{
			"type":  "content_block_delta",
			"index": 0,
			"delta": map[string]any{"type": "text_delta", "text": chunk},
		}
		if err := WriteSSEEvent(w, "content_block_delta", delta); err != nil {
			return err
		}
		flush(fl)
		return nil
	})
	dur := time.Since(start)
	if runErr != nil {
		s.note("/v1/messages", model, false, runErr.Error(), dur)
		_ = WriteSSEEvent(w, "error", map[string]any{
			"type":  "error",
			"error": map[string]any{"type": "upstream_error", "message": runErr.Error()},
		})
		_ = WriteSSEEvent(w, "message_stop", map[string]any{"type": "message_stop"})
		flush(fl)
		return
	}
	s.note("/v1/messages", model, true, "", dur)
	_ = WriteSSEEvent(w, "message_stop", map[string]any{"type": "message_stop"})
	flush(fl)
}

func (s *Server) note(endpoint, model string, ok bool, errMsg string, dur time.Duration) {
	s.mu.Lock()
	s.last = LastCall{
		At:       time.Now(),
		Endpoint: endpoint,
		Model:    model,
		OK:       ok,
		Error:    errMsg,
		Duration: dur,
	}
	s.mu.Unlock()
	if ok {
		log.Printf("%s model=%s duration=%s ok", endpoint, model, dur)
		return
	}
	log.Printf("%s model=%s duration=%s error=%s", endpoint, model, dur, errMsg)
}

func (s *Server) snapshot() (LastCall, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	models := append([]string(nil), s.modelCache...)
	return s.last, models
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, typ, msg string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": msg,
			"type":    typ,
		},
	})
}

func newID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}

func createdUnix() int64 {
	return time.Now().Unix()
}
