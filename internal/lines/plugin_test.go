package lines

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codefree2api/internal/srd"
)

func TestPluginReport(t *testing.T) {
	const remote = "https://example.test/repo"
	var detail int
	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		matchSigned(t, r, srd.Plugin())
		switch r.URL.Path {
		case "/api/composqback/component/v1/detail":
			detail++
			if r.URL.Query().Get("componentId") != "39999" || r.Header.Get("projectId") != "0" {
				t.Fatal("detail")
			}
			_, _ = io.WriteString(w, `{"data":{"latestVersion":"9.8.7"}}`)
		case recordPath:
			if r.Header.Get("projectId") != "0" || r.Header.Get("userId") != "7" || r.Header.Get("gitUrls") != remote {
				t.Fatal("headers")
			}
			raw = append([]byte(nil), body...)
			_, _ = io.WriteString(w, `{"optResult":0}`)
		default:
			t.Fatal("path")
		}
	}))
	defer srv.Close()

	opt, out := pluginOpt(t, srv)
	err := Run(context.Background(), []string{"plugin", "--remote", remote, "--editor-version", "1.99.0", "8", "3"}, opt)
	if err != nil {
		t.Fatal(err)
	}
	if detail != 1 {
		t.Fatal("detail")
	}
	if out.String() != "生成代码采纳行数 8\n新增代码有效总行 3\n" {
		t.Fatal("stdout")
	}
	assertPluginBody(t, raw, "1.99.0", "9.8.7", remote)
}

func TestPluginVersionFlagSkipsLookup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != recordPath {
			t.Fatal("lookup")
		}
		_, _ = io.WriteString(w, `{"optResult":0}`)
	}))
	defer srv.Close()
	opt, out := pluginOpt(t, srv)
	err := Run(context.Background(), []string{"plugin", "--remote", "https://example.test/repo", "--plugin-version", "3.3.1", "--editor-version", "1.99.0", "8", "3"}, opt)
	if err != nil || !strings.Contains(out.String(), "生成代码采纳行数 8\n") {
		t.Fatal(err)
	}
}

func TestPluginRejectsMissingResult(t *testing.T) {
	for _, body := range []string{`{"code":0}`, `{}`, `{"optResult":1}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != recordPath {
				t.Fatal("lookup")
			}
			_, _ = io.WriteString(w, body)
		}))
		opt, out := pluginOpt(t, srv)
		err := Run(context.Background(), []string{"plugin", "--remote", "https://example.test/repo", "--plugin-version", "3.3.1", "--editor-version", "1.99.0", "8", "3"}, opt)
		if err != errReport || out.Len() != 0 {
			t.Fatal("result")
		}
		srv.Close()
	}
}

func TestPluginHTTPErrorStops(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusBadGateway)
	}))
	defer srv.Close()
	opt, out := pluginOpt(t, srv)
	err := Run(context.Background(), []string{"plugin", "--remote", "https://example.test/repo", "--plugin-version", "3.3.1", "--editor-version", "1.99.0", "8", "3"}, opt)
	if err != errReport || out.Len() != 0 {
		t.Fatal("status")
	}
}

func TestPluginUnauthorizedDoesNotLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusUnauthorized)
	}))
	defer srv.Close()
	opt, out := pluginOpt(t, srv)
	err := Run(context.Background(), []string{"plugin", "--remote", "https://example.test/repo", "--plugin-version", "3.3.1", "--editor-version", "1.99.0", "8", "3"}, opt)
	if err != errReport || out.Len() != 0 {
		t.Fatal("login")
	}
}

func TestPluginZeroSkipsNetwork(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("sent a request")
		return nil, nil
	})}
	var out strings.Builder
	err := Run(context.Background(), []string{"plugin", "0", "4"}, Options{
		Dir:    t.TempDir(),
		Stdout: &out,
		Stderr: io.Discard,
		Env:    []string{},
		Client: client,
		Open:   func(string) error { t.Fatal("login"); return nil },
	})
	if err != nil || out.String() != "生成代码采纳行数 0\n新增代码有效总行 4\n" {
		t.Fatal(err)
	}
}

func TestPluginMissingVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == recordPath {
			t.Fatal("posted")
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	opt, out := pluginOpt(t, srv)
	err := Run(context.Background(), []string{"plugin", "--remote", "https://example.test/repo", "--editor-version", "1.99.0", "8", "3"}, opt)
	if err != errNoPluginVersion || out.Len() != 0 {
		t.Fatal(err)
	}
}

func pluginOpt(t *testing.T, srv *httptest.Server) (Options, *strings.Builder) {
	t.Helper()
	var out strings.Builder
	return Options{
		Dir:     t.TempDir(),
		Stdout:  &out,
		Stderr:  io.Discard,
		Env:     []string{},
		Client:  srv.Client(),
		Now:     func() time.Time { return time.Date(2026, 1, 2, 5, 3, 4, 8_000_000, time.Local) },
		Session: &srd.Session{UserID: "7", SessionID: "sess", CFToken: "cft"},
		Open:    func(string) error { t.Fatal("login"); return nil },
		BaseURL: srv.URL,
	}, &out
}

func assertPluginBody(t *testing.T, raw []byte, editor, pluginVersion, remote string) {
	t.Helper()
	if !strings.Contains(string(raw), `"userId":7`) || strings.Contains(string(raw), `"userId":"7"`) {
		t.Fatal("userId")
	}
	if !strings.Contains(string(raw), `"projectName":""`) {
		t.Fatal("projectName")
	}
	var body struct {
		UserID    int64  `json:"userId"`
		ProjectID int64  `json:"projectId"`
		App       string `json:"app"`
		Service   string `json:"service"`
		Time      string `json:"time"`
		Client    struct {
			Type          string   `json:"type"`
			Version       string   `json:"version"`
			PluginVersion string   `json:"pluginVersion"`
			GitURL        string   `json:"gitUrl"`
			GitURLs       []string `json:"gitUrls"`
		} `json:"client"`
		Activities []struct {
			Key    string `json:"key"`
			SubKey string `json:"subKey"`
			Value  struct {
				Count int64 `json:"count"`
			} `json:"value"`
		} `json:"activities"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.UserID != 7 || body.ProjectID != 0 || body.App != "aicode" || body.Service != "agent" || body.Time != "2026-1-2 05:3:4.008" {
		t.Fatal("record")
	}
	if body.Client.Type != "vscode" || body.Client.Version != editor || body.Client.PluginVersion != pluginVersion || body.Client.GitURL != remote || len(body.Client.GitURLs) != 1 || body.Client.GitURLs[0] != remote {
		t.Fatal("client")
	}
	if len(body.Activities) != 2 || body.Activities[0].Key != "agent_accepted_code_line" || body.Activities[0].SubKey != "agent" || body.Activities[0].Value.Count != 8 {
		t.Fatal("line")
	}
	if body.Activities[1].Key != "agent_accepted_code_count" || body.Activities[1].SubKey != "agent" || body.Activities[1].Value.Count != 1 {
		t.Fatal("count")
	}
}

func matchSigned(t *testing.T, r *http.Request, app srd.App) {
	t.Helper()
	if r.Header.Get("User-Agent") != "codefree2api" || r.Header.Get("X-Cf-AppId") != app.ClientID || r.Header.Get("X-Cf-Token") == "" {
		t.Fatal("auth")
	}
	for key, values := range r.Header {
		if strings.Contains(strings.ToLower(key), "secret") {
			t.Fatal("secret")
		}
		for _, value := range values {
			if value == app.ClientSecret || value == app.Signing {
				t.Fatal("secret")
			}
		}
	}
	want, err := srd.Sign(r.Method, app.ClientID, app.Signing, r.Header.Get("X-Cf-Timestamp"), r.Header.Get("X-Cf-Nonce"))
	if err != nil || want == "" || want != r.Header.Get("X-Cf-Signature") {
		t.Fatal("signature")
	}
}
