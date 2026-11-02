package proxy

import (
	"context"
	"encoding/json"
)

// Turn is one client request, independent of OpenAI, Anthropic, or Responses.
type Turn struct {
	Model      string
	System     string
	Transcript string
	Files      []FileRef
	Tools      []ToolDef
	ToolChoice ToolChoice
	Variant    string
	MaxTokens  int
	Stop       []string
	// SessionID continues an upstream codefree session (Responses previous_response_id).
	SessionID string
}

type ToolDef struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// ToolChoice.Mode is auto, none, required, or tool.
type ToolChoice struct {
	Mode string
	Name string
}

type FileRef struct {
	MIME string
	Name string
	URL  string
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

type Usage struct {
	Input      int
	Output     int
	Reasoning  int
	CacheRead  int
	CacheWrite int
}

func (u Usage) Completion() int { return u.Output + u.Reasoning }

func (u Usage) Total() int { return u.Input + u.Completion() }

// Delta is a streamed piece of visible text or reasoning.
type Delta struct {
	Text      string
	Reasoning string
}

type RunRequest struct {
	ProviderID  string
	ModelID     string
	System      string
	Prompt      string
	Files       []FileRef
	Variant     string
	ClientTools bool
	Schema      map[string]any
	SessionID   string
}

type RunResult struct {
	SessionID string
	Text      string
	Reasoning string
	Tools     []ToolCall
	Usage     Usage
	// Finish is stop, tool_calls, length, content_filter, or stop_sequence.
	Finish    string
	StopMatch string
}

type ModelInfo struct {
	ID string
}

// Engine talks to the local codefree-o server.
type Engine interface {
	Models(ctx context.Context) ([]ModelInfo, error)
	Run(ctx context.Context, req RunRequest, emit func(Delta) error) (RunResult, error)
}
