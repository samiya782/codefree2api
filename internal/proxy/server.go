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

const maxBody = 32 << 20

type Options struct {
	Port     int
	Password string
	Bin      string
	Aliases  map[string]string
}

type LastCall struct {
	At           time.Time
	Endpoint     string
	Model        string
	OK           bool
	Error        string
	Duration     time.Duration
	InputTokens  int
	OutputTokens int
}

type Stats struct {
	Requests     int `json:"requests"`
	OK           int `json:"ok"`
	Errors       int `json:"errors"`
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type Server struct {
	addr     string
	password string
	bin      string
	workDir  string
	aliases  map[string]string
	engine   Engine
	mux      *http.ServeMux

	mu           sync.Mutex
	last         LastCall
	stats        Stats
	modelCache   []string
	sessions     map[string]string
	sessionOrder []string
}

func New(opt Options) (*Server, error) {
	if opt.Bin == "" {
		opt.Bin = DefaultBin
	}
	dir := filepath.Join(os.TempDir(), "codefree2api-work")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("work dir: %w", err)
	}
	return newServer(opt, &Codefree{Bin: opt.Bin, Dir: dir})
}

func newServer(opt Options, eng Engine) (*Server, error) {
	if opt.Port <= 0 {
		opt.Port = DefaultPort
	}
	if opt.Bin == "" {
		opt.Bin = DefaultBin
	}
	dir := filepath.Join(os.TempDir(), "codefree2api-work")
	if cf, ok := eng.(*Codefree); ok && cf.Dir != "" {
		dir = cf.Dir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("work dir: %w", err)
	}
	aliases := map[string]string{}
	for k, v := range opt.Aliases {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k != "" && v != "" {
			aliases[k] = v
		}
	}
	s := &Server{
		addr:     net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", opt.Port)),
		password: opt.Password,
		bin:      opt.Bin,
		workDir:  dir,
		aliases:  aliases,
		engine:   eng,
		mux:      http.NewServeMux(),
		sessions: map[string]string{},
	}
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /v1/models", s.handleModels)
	s.mux.HandleFunc("POST /v1/chat/completions", s.handleChat)
	s.mux.HandleFunc("POST /v1/responses", s.handleResponses)
	s.mux.HandleFunc("POST /v1/messages/count_tokens", s.handleCountTokens)
	s.mux.HandleFunc("POST /v1/messages", s.handleMessages)
	s.mux.HandleFunc("GET /admin", s.handleAdmin)
	s.mux.HandleFunc("GET /api/stats", s.handleStats)
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
		if strings.Contains(r.URL.Path, "/messages") {
			writeAnthropicError(w, http.StatusUnauthorized, "authentication_error", "unauthorized: missing or invalid API key")
			return
		}
		writeOpenAIError(w, http.StatusUnauthorized, "auth_error", "", "unauthorized: missing or invalid Bearer token")
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) authorized(r *http.Request) bool {
	if s.password == "" {
		return true
	}
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" {
		token = strings.TrimSpace(r.Header.Get("x-api-key"))
	}
	if token == "" {
		return false
	}
	return secureEqual(token, s.password)
}

func bearerToken(h string) string {
	scheme, token, ok := strings.Cut(strings.TrimSpace(h), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
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

func (s *Server) resolveModel(model string) string {
	model = strings.TrimSpace(model)
	if alias, ok := s.aliases[model]; ok && alias != "" {
		return alias
	}
	return model
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), listTimeout)
	defer cancel()
	start := time.Now()
	models, err := s.engine.Models(ctx)
	if err != nil {
		s.note("/v1/models", "", false, err.Error(), time.Since(start), Usage{})
		writeOpenAIError(w, http.StatusBadGateway, "upstream_error", "", err.Error())
		return
	}
	ids := make([]string, 0, len(models))
	for _, m := range models {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	s.mu.Lock()
	s.modelCache = append([]string(nil), ids...)
	s.mu.Unlock()
	s.note("/v1/models", "", true, "", time.Since(start), Usage{})

	created := createdUnix()
	data := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		data = append(data, map[string]any{
			"id":       id,
			"object":   "model",
			"created":  created,
			"owned_by": "codefree",
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": data})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	stats := s.stats
	last := s.last
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"requests":      stats.Requests,
		"ok":            stats.OK,
		"errors":        stats.Errors,
		"input_tokens":  stats.InputTokens,
		"output_tokens": stats.OutputTokens,
		"last": map[string]any{
			"endpoint":      last.Endpoint,
			"model":         last.Model,
			"ok":            last.OK,
			"error":         last.Error,
			"input_tokens":  last.InputTokens,
			"output_tokens": last.OutputTokens,
		},
	})
}

func (s *Server) note(endpoint, model string, ok bool, errMsg string, dur time.Duration, usage Usage) {
	s.mu.Lock()
	s.last = LastCall{
		At:           time.Now(),
		Endpoint:     endpoint,
		Model:        model,
		OK:           ok,
		Error:        errMsg,
		Duration:     dur,
		InputTokens:  usage.Input,
		OutputTokens: usage.Completion(),
	}
	s.stats.Requests++
	s.stats.InputTokens += usage.Input
	s.stats.OutputTokens += usage.Completion()
	if ok {
		s.stats.OK++
	} else {
		s.stats.Errors++
	}
	s.mu.Unlock()
	if ok {
		log.Printf("%s model=%s duration=%s tokens_in=%d tokens_out=%d ok", endpoint, model, dur, usage.Input, usage.Completion())
		return
	}
	log.Printf("%s model=%s duration=%s error=%s", endpoint, model, dur, errMsg)
}

func (s *Server) snapshot() (LastCall, []string, Stats) {
	s.mu.Lock()
	defer s.mu.Unlock()
	models := append([]string(nil), s.modelCache...)
	return s.last, models, s.stats
}

func (s *Server) remember(respID, sessionID string) {
	if respID == "" || sessionID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[respID]; !ok {
		s.sessionOrder = append(s.sessionOrder, respID)
	}
	s.sessions[respID] = sessionID
	for len(s.sessionOrder) > 256 {
		old := s.sessionOrder[0]
		s.sessionOrder = s.sessionOrder[1:]
		delete(s.sessions, old)
	}
}

func (s *Server) lookupSession(respID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sid, ok := s.sessions[respID]
	return sid, ok && sid != ""
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, typ, msg string) {
	writeOpenAIError(w, status, typ, "", msg)
}

func newID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}

func createdUnix() int64 { return time.Now().Unix() }
