package proxy

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"codefree2api/internal/srd"
)

func TestSpaceSSE(t *testing.T) {
	var sawTools bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case projectPath, releasesPath:
			t.Fatal("lookup")
		case completionsPath:
			if r.Header.Get("subService") != "codefree-ide-agent" || r.Header.Get("modelName") != "GLM-5.2" || r.Header.Get("X-Codefree-Quota-Schema") != "1" {
				t.Fatal("headers")
			}
			if r.Header.Get("projectId") != "0" || r.Header.Get("User-Agent") != "codefree2api" {
				t.Fatal("headers")
			}
			matchProxySign(t, r, srd.Space(), "POST")
			if !strings.Contains(string(body), `"stream":true`) || !strings.Contains(string(body), `"name":"echo"`) || strings.Contains(string(body), `"tool_choice"`) {
				t.Fatal("body")
			}
			sawTools = true
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"he\"}}]}\n\n")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"llo\",\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"function\":{\"name\":\"echo\",\"arguments\":\"{\\\"a\\\":\"}}]}}]}\n\n")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"1}\"}}]},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":3}}\n\n")
			_, _ = io.WriteString(w, ": ping\n\ndata: [DONE]\n")
		default:
			t.Fatal("path")
		}
	}))
	defer srv.Close()
	eng := testSpace(t, srv, t.TempDir())
	var streamed strings.Builder
	res, err := eng.Run(context.Background(), RunRequest{
		ModelID:    "GLM-5.2",
		System:     "sys",
		Prompt:     "hi",
		Tools:      []ToolDef{{Name: "echo", Parameters: json.RawMessage(`{"type":"object"}`)}},
		ToolChoice: ToolChoice{Mode: "auto"},
	}, func(d Delta) error {
		streamed.WriteString(d.Text)
		return nil
	})
	if err != nil || !sawTools {
		t.Fatal(err)
	}
	if res.Text != "hello" || streamed.String() != "hello" || res.Finish != "tool_calls" || res.Usage.Input != 2 || res.Usage.Output != 3 {
		t.Fatal("result")
	}
	if len(res.Tools) != 1 || res.Tools[0].ID != "c1" || res.Tools[0].Name != "echo" || res.Tools[0].Arguments != `{"a":1}` {
		t.Fatal("tools")
	}
}

func TestSpaceUnauthorizedRetriesOnce(t *testing.T) {
	var calls int
	var logins int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != completionsPath {
			t.Fatal("path")
		}
		calls++
		if calls == 1 {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"pong\"}}]}\n\ndata: [DONE]\n")
	}))
	defer srv.Close()
	eng := testSpace(t, srv, t.TempDir())
	eng.Login = func(context.Context) (*srd.Session, error) {
		logins++
		return &srd.Session{UserID: "8", CFToken: "next"}, nil
	}
	res, err := eng.Run(context.Background(), RunRequest{ModelID: "GLM-5.2", Prompt: "hi"}, nil)
	if err != nil || res.Text != "pong" || calls != 2 || logins != 1 {
		t.Fatal("retry")
	}
}

func TestSpaceQuotaAndEmptyModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case completionsPath:
			http.Error(w, "no", http.StatusTooManyRequests)
		case modelsPath:
			_, _ = io.WriteString(w, `[]`)
		default:
			t.Fatal("path")
		}
	}))
	defer srv.Close()
	eng := testSpace(t, srv, t.TempDir())
	_, err := eng.Run(context.Background(), RunRequest{ModelID: "GLM-5.2", Prompt: "hi"}, nil)
	if !errors.Is(err, errQuota) {
		t.Fatal("quota")
	}
	_, err = eng.Models(context.Background())
	if !errors.Is(err, errNoModels) {
		t.Fatal("models")
	}
}

func TestSpaceModelsRetryAndProjectOnce(t *testing.T) {
	repo := gitRepo(t)
	var models int
	var projects int
	var releases int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case modelsPath:
			models++
			if models == 1 {
				http.Error(w, "no", http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `{"models":[{"id":"GLM-5.2"}]}`)
		case releasesPath:
			releases++
			_, _ = io.WriteString(w, `[{"version":"9.9.9"}]`)
		case projectPath:
			projects++
			if r.Header.Get("projectId") != "0" {
				t.Fatal("project")
			}
			_, _ = io.WriteString(w, `{"project":{"id":12,"name":"Demo"}}`)
		case completionsPath:
			if r.Header.Get("projectId") != "12" {
				t.Fatal("project header")
			}
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\ndata: [DONE]\n")
		default:
			t.Fatal("path")
		}
	}))
	defer srv.Close()
	eng := testSpace(t, srv, repo)
	var logins int
	eng.Login = func(context.Context) (*srd.Session, error) {
		logins++
		return &srd.Session{UserID: "8", CFToken: "next"}, nil
	}
	got, err := eng.Models(context.Background())
	if err != nil || len(got) != 1 || got[0].ID != "GLM-5.2" || models != 2 || logins != 1 {
		t.Fatal("models")
	}
	if _, err := eng.Run(context.Background(), RunRequest{ModelID: "GLM-5.2", Prompt: "hi"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Run(context.Background(), RunRequest{ModelID: "GLM-5.2", Prompt: "hi"}, nil); err != nil {
		t.Fatal(err)
	}
	if projects != 1 || releases != 1 {
		t.Fatal("once")
	}
}

func TestSpaceVersionSkipsLookup(t *testing.T) {
	repo := gitRepo(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case releasesPath:
			t.Fatal("lookup")
		case projectPath:
			if !strings.Contains(string(body), `"version":"4.0.0"`) {
				t.Fatal("version")
			}
			_, _ = io.WriteString(w, `{"project":{"id":3,"name":"P"}}`)
		case completionsPath:
			_, _ = io.WriteString(w, "data: [DONE]\n")
		default:
			t.Fatal("path")
		}
	}))
	defer srv.Close()
	eng := testSpace(t, srv, repo)
	eng.Version = "4.0.0"
	if _, err := eng.Run(context.Background(), RunRequest{ModelID: "GLM-5.2", Prompt: "hi"}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestPluginWebSocket(t *testing.T) {
	wsURL := pluginSocket(t, func(msg wsMessage) []wsMessage {
		switch msg.MessageName {
		case "RegisterChannel":
			if msg.Context["appGId"] != "aicode" || msg.Context["version"] != "1.99.0" || msg.Context["projectId"] != "0" {
				t.Fatal("register")
			}
			if _, ok := msg.Context["userId"]; ok {
				t.Fatal("userId")
			}
			matchContextSign(t, msg, "REGISTERCHANNEL")
			return []wsMessage{{MessageName: "RegisterChannel_resp", Context: map[string]any{"optResult": 0}}}
		case "CodeChatRequest":
			matchContextSign(t, msg, "CODECHATREQUEST")
			payload := msg.Payload
			if payload["clientType"] != "vscode" || payload["clientVersion"] != "1.99.0" || payload["pluginVersion"] != "3.3.1" || payload["clientPlatform"] != clientPlatform() || payload["model"] != "GLM-5.2" {
				t.Fatal("payload")
			}
			messages, _ := payload["messages"].(map[string]any)
			if messages["sub_service"] != "assistant" {
				t.Fatal("sub")
			}
			return []wsMessage{
				{MessageName: "CodeChatRequest_resp", Payload: map[string]any{"answer": "hel"}},
				{MessageName: "CodeChatRequest_resp", Payload: map[string]any{"answer": "hello", "isEnd": 1}},
			}
		default:
			t.Fatal("message")
			return nil
		}
	})
	eng := testPlugin(t, refuseHTTP(t), wsURL)
	var streamed strings.Builder
	res, err := eng.Run(context.Background(), RunRequest{ModelID: "GLM-5.2", System: "sys", Prompt: "hi"}, func(d Delta) error {
		streamed.WriteString(d.Text)
		return nil
	})
	if err != nil || res.Text != "hello" || streamed.String() != "hello" || res.Finish != "stop" {
		t.Fatal("chat")
	}
}

func TestPluginToolEnvelopeIsNotStreamed(t *testing.T) {
	wsURL := pluginSocket(t, func(msg wsMessage) []wsMessage {
		if msg.MessageName == "RegisterChannel" {
			return []wsMessage{{MessageName: "RegisterChannel_resp", Context: map[string]any{"optResult": 0}}}
		}
		raw := `{"content":"hi","tool_calls":[{"name":"echo","arguments":{"x":1}}]}`
		return []wsMessage{{MessageName: "CodeChatRequest_resp", Payload: map[string]any{"answer": raw, "isEnd": 1}}}
	})
	eng := testPlugin(t, refuseHTTP(t), wsURL)
	var streamed strings.Builder
	res, err := eng.Run(context.Background(), RunRequest{ModelID: "GLM-5.2", Prompt: "hi", ClientTools: true}, func(d Delta) error {
		streamed.WriteString(d.Text)
		return nil
	})
	if err != nil || streamed.Len() != 0 || res.Text != "hi" || res.Finish != "tool_calls" || len(res.Tools) != 1 || res.Tools[0].Name != "echo" || res.Tools[0].Arguments != `{"x":1}` {
		t.Fatal("envelope")
	}
}

func TestPluginRegisterUnauthorizedRetriesOnce(t *testing.T) {
	var conns atomic.Int32
	wsURL := pluginSocket(t, func(msg wsMessage) []wsMessage {
		if msg.MessageName != "RegisterChannel" {
			return []wsMessage{{MessageName: "CodeChatRequest_resp", Payload: map[string]any{"answer": "pong", "isEnd": 1}}}
		}
		n := conns.Add(1)
		if n == 1 {
			return []wsMessage{{MessageName: "RegisterChannel_resp", Context: map[string]any{"optResult": 401}}}
		}
		return []wsMessage{{MessageName: "RegisterChannel_resp", Context: map[string]any{"optResult": 0}}}
	})
	eng := testPlugin(t, refuseHTTP(t), wsURL)
	var logins int
	eng.Login = func(context.Context) (*srd.Session, error) {
		logins++
		return &srd.Session{UserID: "8", CFToken: "next"}, nil
	}
	res, err := eng.Run(context.Background(), RunRequest{ModelID: "GLM-5.2", Prompt: "hi"}, nil)
	if err != nil || res.Text != "pong" || logins != 1 || conns.Load() != 2 {
		t.Fatal("retry")
	}
}

func TestPluginVersionLookupRetriesOnce(t *testing.T) {
	var detail int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != detailPath {
			t.Fatal("path")
		}
		detail++
		if detail == 1 {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"latestVersion":"9.8.7"}}`)
	}))
	defer srv.Close()
	wsURL := pluginSocket(t, func(msg wsMessage) []wsMessage {
		if msg.MessageName == "RegisterChannel" {
			return []wsMessage{{MessageName: "RegisterChannel_resp", Context: map[string]any{"optResult": 0}}}
		}
		if msg.Payload["pluginVersion"] != "9.8.7" {
			t.Fatal("version")
		}
		return []wsMessage{{MessageName: "CodeChatRequest_resp", Payload: map[string]any{"answer": "ok", "isEnd": 1}}}
	})
	eng := testPlugin(t, srv.URL, wsURL)
	eng.HTTP = srv.Client()
	eng.PluginVersion = ""
	var logins int
	eng.Login = func(context.Context) (*srd.Session, error) {
		logins++
		return &srd.Session{UserID: "8", CFToken: "next"}, nil
	}
	res, err := eng.Run(context.Background(), RunRequest{ModelID: "GLM-5.2", Prompt: "hi"}, nil)
	if err != nil || res.Text != "ok" || detail != 2 || logins != 1 {
		t.Fatal("version")
	}
}

func testSpace(t *testing.T, srv *httptest.Server, dir string) *Space {
	t.Helper()
	app := srd.Space()
	app.Host = srv.URL
	return &Space{
		remote: remote{
			App:     app,
			HTTP:    srv.Client(),
			Now:     func() time.Time { return time.Unix(1_700_000_000, 0) },
			Session: &srd.Session{UserID: "7", CFToken: "cft"},
			Login: func(context.Context) (*srd.Session, error) {
				t.Fatal("login")
				return nil, errAuth
			},
		},
		Dir: dir,
	}
}

func testPlugin(t *testing.T, host, wsURL string) *Plugin {
	t.Helper()
	app := srd.Plugin()
	app.Host = host
	return &Plugin{
		remote: remote{
			App:     app,
			HTTP:    http.DefaultClient,
			Now:     func() time.Time { return time.Unix(1_700_000_000, 0) },
			Session: &srd.Session{UserID: "7", CFToken: "cft"},
			Login: func(context.Context) (*srd.Session, error) {
				t.Fatal("login")
				return nil, errAuth
			},
		},
		WSURL:         wsURL,
		EditorVersion: "1.99.0",
		PluginVersion: "3.3.1",
		Dir:           t.TempDir(),
	}
}

func matchProxySign(t *testing.T, r *http.Request, app srd.App, method string) {
	t.Helper()
	want, err := srd.Sign(method, app.ClientID, app.Signing, r.Header.Get("X-Cf-Timestamp"), r.Header.Get("X-Cf-Nonce"))
	if err != nil || r.Header.Get("X-Cf-AppId") != app.ClientID || want == "" || want != r.Header.Get("X-Cf-Signature") {
		t.Fatal("signature")
	}
	if r.Header.Get("X-Cf-Token") == app.Signing || r.Header.Get("X-Cf-Signature") == app.ClientSecret {
		t.Fatal("secret")
	}
}

func matchContextSign(t *testing.T, msg wsMessage, method string) {
	t.Helper()
	app := srd.Plugin()
	ts, _ := msg.Context["X-Cf-Timestamp"].(string)
	nonce, _ := msg.Context["X-Cf-Nonce"].(string)
	got, _ := msg.Context["X-Cf-Signature"].(string)
	appID, _ := msg.Context["X-Cf-AppId"].(string)
	want, err := srd.Sign(method, app.ClientID, app.Signing, ts, nonce)
	if err != nil || appID != app.ClientID || want == "" || got != want {
		t.Fatal("signature")
	}
}

func pluginSocket(t *testing.T, handle func(wsMessage) []wsMessage) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("hijack")
		}
		conn, rw, err := hj.Hijack()
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		key := r.Header.Get("Sec-WebSocket-Key")
		sum := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
		accept := base64.StdEncoding.EncodeToString(sum[:])
		fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept)
		if err := rw.Flush(); err != nil {
			t.Fatal(err)
		}
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		for {
			msg, err := readClientChannel(rw.Reader)
			if err != nil {
				return
			}
			for _, reply := range handle(msg) {
				if err := writeServerChannel(rw.Writer, reply); err != nil {
					t.Fatal(err)
				}
			}
			if err := rw.Flush(); err != nil {
				t.Fatal(err)
			}
		}
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + wsPath
}

func readClientChannel(r *bufio.Reader) (wsMessage, error) {
	var buf string
	for {
		payload, err := readMaskedText(r)
		if err != nil {
			return wsMessage{}, err
		}
		buf += payload
		start := strings.Index(buf, "<WBChannel>")
		end := strings.Index(buf, "</WBChannel>")
		if start < 0 || end < 0 || end < start {
			continue
		}
		raw := buf[start+len("<WBChannel>") : end]
		dec := json.NewDecoder(strings.NewReader(raw))
		dec.UseNumber()
		var msg wsMessage
		if err := dec.Decode(&msg); err != nil {
			return wsMessage{}, err
		}
		if msg.Context == nil {
			msg.Context = map[string]any{}
		}
		if msg.Payload == nil {
			msg.Payload = map[string]any{}
		}
		return msg, nil
	}
}

func readMaskedText(r *bufio.Reader) (string, error) {
	b0, err := r.ReadByte()
	if err != nil {
		return "", err
	}
	b1, err := r.ReadByte()
	if err != nil {
		return "", err
	}
	ln := uint64(b1 & 0x7f)
	switch ln {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return "", err
		}
		ln = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return "", err
		}
		ln = binary.BigEndian.Uint64(ext[:])
	}
	var key [4]byte
	if b1&0x80 != 0 {
		if _, err = io.ReadFull(r, key[:]); err != nil {
			return "", err
		}
	}
	payload := make([]byte, ln)
	if _, err = io.ReadFull(r, payload); err != nil {
		return "", err
	}
	if b1&0x80 != 0 {
		for i := range payload {
			payload[i] ^= key[i%4]
		}
	}
	if b0&0x0f != 0x1 {
		return "", nil
	}
	return string(payload), nil
}

func writeServerChannel(w io.Writer, msg wsMessage) error {
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	payload := append([]byte("<WBChannel>"), raw...)
	payload = append(payload, []byte("</WBChannel>")...)
	header := []byte{0x81, 0}
	n := len(payload)
	switch {
	case n < 126:
		header[1] |= byte(n)
	default:
		header[1] |= 126
		header = append(header, byte(n>>8), byte(n))
	}
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err = w.Write(payload)
	return err
}

func refuseHTTP(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("http")
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		if _, err := cmd.CombinedOutput(); err != nil {
			t.Fatal("git")
		}
	}
	run("init")
	run("remote", "add", "origin", "https://example.test/repo")
	return dir
}
