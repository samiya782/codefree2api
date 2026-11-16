package lines

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRunZeroSkipsNetwork(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("sent a request")
		return nil, nil
	})}
	var out strings.Builder
	err := Run(context.Background(), []string{"0", "12"}, Options{
		Dir:     t.TempDir(),
		Stdout:  &out,
		Env:     []string{},
		Client:  client,
		Version: "1.10.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "生成代码采纳行数 0\n新增代码有效总行 12\n" {
		t.Fatalf("stdout %q", out.String())
	}
}

func TestRunRejectsIncompleteNumbers(t *testing.T) {
	err := Run(context.Background(), []string{"4"}, Options{Env: []string{}, Version: "1.10.1"})
	if err != errUsage {
		t.Fatalf("got %v", err)
	}
}

func TestRunReportsFourActivities(t *testing.T) {
	var got []string
	var refreshed bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case tokenPath:
			refreshed = true
			if r.Header.Get("User-Agent") != "opencode/1.10.1" {
				t.Errorf("user agent %s", r.Header.Get("User-Agent"))
			}
			if !strings.Contains(string(body), "grant_type=refresh_x_cf_token") {
				t.Errorf("refresh body missing grant")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"x_cf_token":"access-1","x_cf_token_expires_in":"1800","x_cf_refresh_token":"refresh-2","x_cf_refresh_token_expires_in":"604800"}`)
		case recordPath:
			if r.Header.Get("X-Cf-Token") == "" || r.Header.Get("X-Cf-Signature") == "" {
				t.Errorf("missing signature headers")
			}
			var parsed recordBody
			if err := json.Unmarshal(body, &parsed); err != nil {
				t.Fatal(err)
			}
			if parsed.App != "codefree-o" || parsed.Client.Type != "codefree-o" || parsed.Client.Version != "1.10.1" {
				t.Fatalf("identity %#v", parsed)
			}
			if len(parsed.Activities) != 1 || parsed.Activities[0].SubKey != "edit" {
				t.Fatalf("activity %#v", parsed.Activities)
			}
			if len(parsed.Client.GitURLs) != 1 || parsed.Client.GitURLs[0] != "https://example.test/repo" {
				t.Fatalf("git urls %#v", parsed.Client.GitURLs)
			}
			got = append(got, parsed.Activities[0].Key+":"+strconv.FormatInt(parsed.Activities[0].Value.Count, 10))
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, "{\"optResult\":0}\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	home := t.TempDir()
	authPath := filepath.Join(home, ".codefree-o", ".local", "share", "codefree.json")
	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestAuth(t, authPath, srv.URL, "refresh-1", "358637")
	repo := gitRepoWithRemote(t)

	var out strings.Builder
	err := Run(context.Background(), []string{"8", "3"}, Options{
		Dir:     repo,
		Stdout:  &out,
		Env:     []string{"OPENCODE_TEST_HOME=" + home},
		Client:  srv.Client(),
		Now:     func() time.Time { return time.Date(2026, 11, 16, 5, 8, 16, 822000000, time.Local) },
		Version: "1.10.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !refreshed {
		t.Fatal("did not refresh")
	}
	want := []string{
		"tools_gen_code_count:1",
		"tools_gen_code_line:8",
		"tools_gen_accepted_code_line:8",
		"tools_gen_accepted_code_count:1",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("activities %v", got)
	}
	if out.String() != "生成代码采纳行数 8\n新增代码有效总行 3\n" {
		t.Fatalf("stdout %q", out.String())
	}
	saved, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "refresh-2") || strings.Contains(string(saved), "access-1") {
		t.Fatal("auth file contains a plaintext token")
	}
	plain, err := decryptRefresh(encryptedField(t, saved))
	if err != nil || plain != "refresh-2" {
		t.Fatalf("stored refresh %q %v", plain, err)
	}
}

func TestRunStopsAfterRejectedActivity(t *testing.T) {
	var n int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == tokenPath {
			_, _ = io.WriteString(w, `{"x_cf_token":"access-1","x_cf_refresh_token":"refresh-2","x_cf_refresh_token_expires_in":"60"}`)
			return
		}
		n++
		w.Header().Set("Content-Type", "application/json")
		if n == 3 {
			_, _ = io.WriteString(w, `{"optResult":1}`)
			return
		}
		_, _ = io.WriteString(w, `{"optResult":0}`)
	}))
	defer srv.Close()
	home := t.TempDir()
	authPath := filepath.Join(home, ".codefree-o", ".local", "share", "codefree.json")
	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestAuth(t, authPath, srv.URL, "refresh-1", "7")
	var out strings.Builder
	err := Run(context.Background(), []string{"4", "1"}, Options{
		Dir:     gitRepoWithRemote(t),
		Stdout:  &out,
		Env:     []string{"OPENCODE_TEST_HOME=" + home},
		Client:  srv.Client(),
		Version: "1.10.1",
	})
	if err != errReport {
		t.Fatalf("got %v", err)
	}
	if n != 3 {
		t.Fatalf("posts %d", n)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout %q", out.String())
	}
}

func TestRunExplicitRemoteFromPlainDirectory(t *testing.T) {
	var urls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path == tokenPath {
			_, _ = io.WriteString(w, `{"x_cf_token":"access-1","x_cf_refresh_token":"refresh-2","x_cf_refresh_token_expires_in":"60"}`)
			return
		}
		var parsed recordBody
		if err := json.Unmarshal(body, &parsed); err != nil {
			t.Fatal(err)
		}
		if len(urls) == 0 {
			urls = append([]string(nil), parsed.Client.GitURLs...)
		}
		_, _ = io.WriteString(w, `{"optResult":0}`)
	}))
	defer srv.Close()
	home := t.TempDir()
	authPath := filepath.Join(home, ".codefree-o", ".local", "share", "codefree.json")
	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestAuth(t, authPath, srv.URL, "refresh-1", "7")
	var out strings.Builder
	err := Run(context.Background(), []string{
		"--remote", " https://custom.test/a ",
		"--remote=https://custom.test/b",
		"--remote", "https://custom.test/a",
		"8", "3",
	}, Options{
		Dir:     t.TempDir(),
		Stdout:  &out,
		Env:     []string{"OPENCODE_TEST_HOME=" + home},
		Client:  srv.Client(),
		Version: "1.10.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(urls, ",") != "https://custom.test/a,https://custom.test/b" {
		t.Fatalf("git urls %#v", urls)
	}
	if out.String() != "生成代码采纳行数 8\n新增代码有效总行 3\n" {
		t.Fatalf("stdout %q", out.String())
	}
}

func TestRunExplicitRemoteReplacesRepoRemotes(t *testing.T) {
	var urls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path == tokenPath {
			_, _ = io.WriteString(w, `{"x_cf_token":"access-1","x_cf_refresh_token":"refresh-2","x_cf_refresh_token_expires_in":"60"}`)
			return
		}
		var parsed recordBody
		if err := json.Unmarshal(body, &parsed); err != nil {
			t.Fatal(err)
		}
		urls = append([]string(nil), parsed.Client.GitURLs...)
		_, _ = io.WriteString(w, `{"optResult":0}`)
	}))
	defer srv.Close()
	home := t.TempDir()
	authPath := filepath.Join(home, ".codefree-o", ".local", "share", "codefree.json")
	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestAuth(t, authPath, srv.URL, "refresh-1", "7")
	var out strings.Builder
	err := Run(context.Background(), []string{"1", "--remote", "https://custom.test/only", "2"}, Options{
		Dir:     gitRepoWithRemote(t),
		Stdout:  &out,
		Env:     []string{"OPENCODE_TEST_HOME=" + home},
		Client:  srv.Client(),
		Version: "1.10.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 1 || urls[0] != "https://custom.test/only" {
		t.Fatalf("git urls %#v", urls)
	}
}

func TestRunRequiresBothNumbers(t *testing.T) {
	var out strings.Builder
	err := Run(context.Background(), []string{"--remote", "https://custom.test/a"}, Options{
		Dir:     t.TempDir(),
		Stdout:  &out,
		Env:     []string{},
		Version: "1.10.1",
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("sent a request")
			return nil, nil
		})},
	})
	if err != errUsage {
		t.Fatalf("got %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout %q", out.String())
	}
}

func TestRunEmptyRemote(t *testing.T) {
	var out strings.Builder
	err := Run(context.Background(), []string{"--remote", "  ", "4", "1"}, Options{
		Dir:     t.TempDir(),
		Stdout:  &out,
		Env:     []string{},
		Version: "1.10.1",
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("sent a request")
			return nil, nil
		})},
	})
	if err != errEmptyRemote {
		t.Fatalf("got %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout %q", out.String())
	}
}

func TestRunNoRemoteDoesNotSend(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	var out strings.Builder
	err := Run(context.Background(), []string{"4", "1"}, Options{
		Dir:     dir,
		Stdout:  &out,
		Env:     []string{},
		Version: "1.10.1",
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("sent a request")
			return nil, nil
		})},
	})
	if err != errNoRemote {
		t.Fatalf("got %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("stdout %q", out.String())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func writeTestAuth(t *testing.T, path, baseURL, refresh, userID string) {
	t.Helper()
	rec := authFile{
		Version:                 2,
		EncryptedRefreshCfToken: encryptRefresh(refresh),
		RefreshCfTokenExpiresAt: time.Now().Add(time.Hour).UnixMilli(),
		UserID:                  userID,
		BaseURLSnapshot:         baseURL,
	}
	if err := writeAuthFile(path, rec); err != nil {
		t.Fatal(err)
	}
}

func encryptedField(t *testing.T, data []byte) string {
	t.Helper()
	var rec authFile
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatal(err)
	}
	return rec.EncryptedRefreshCfToken
}

func gitRepoWithRemote(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "remote", "add", "origin", "https://example.test/repo")
	runGit(t, dir, "remote", "add", "mirror", "https://example.test/repo")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=lines",
		"GIT_AUTHOR_EMAIL=lines@example.com",
		"GIT_COMMITTER_NAME=lines",
		"GIT_COMMITTER_EMAIL=lines@example.com",
		"GIT_TERMINAL_PROMPT=0",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}
