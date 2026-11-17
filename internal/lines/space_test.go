package lines

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"codefree2api/internal/srd"
)

func TestSpaceReport(t *testing.T) {
	const remote = "https://example.test/repo"
	var paths []string
	var records []map[string]any
	var projectBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		matchSigned(t, r, srd.Space())
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/api/acbackend/ideservice/v1/ide-releases":
			if r.URL.Query().Get("onlyLast") != "true" || r.Header.Get("projectId") != "0" {
				t.Fatal("releases")
			}
			_, _ = io.WriteString(w, `[{"version":"9.9.9"}]`)
		case "/api/acbackend/usermanager/v1/users/project":
			if r.Header.Get("projectId") != "0" {
				t.Fatal("project header")
			}
			projectBody = string(body)
			_, _ = io.WriteString(w, `{"project":{"id":12,"name":"Demo"}}`)
		case recordPath:
			dec := json.NewDecoder(strings.NewReader(string(body)))
			dec.UseNumber()
			var rec map[string]any
			if err := dec.Decode(&rec); err != nil {
				t.Fatal(err)
			}
			records = append(records, rec)
			if len(records)%2 == 1 {
				_, _ = io.WriteString(w, `{"code":0}`)
			} else {
				_, _ = io.WriteString(w, `{"optResult":0}`)
			}
		default:
			t.Fatal("path")
		}
	}))
	defer srv.Close()

	opt, out := pluginOpt(t, srv)
	err := Run(context.Background(), []string{"space", "--remote", remote, "8", "3"}, opt)
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != "生成代码采纳行数 8\n新增代码有效总行 3\n" {
		t.Fatal("stdout")
	}
	if strings.Join(paths, ",") != strings.Join([]string{
		"/api/acbackend/ideservice/v1/ide-releases",
		"/api/acbackend/usermanager/v1/users/project",
		recordPath, recordPath, recordPath, recordPath,
	}, ",") {
		t.Fatal("order")
	}
	if !strings.Contains(projectBody, `"clientType":"codefree-ide"`) || !strings.Contains(projectBody, `"version":"9.9.9"`) || !strings.Contains(projectBody, remote) {
		t.Fatal("project")
	}
	assertSpaceRecords(t, records, "9.9.9", 12, "Demo", remote)
}

func TestSpaceVersionFlagSkipsLookup(t *testing.T) {
	var version string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/api/acbackend/ideservice/v1/ide-releases":
			t.Fatal("lookup")
		case "/api/acbackend/usermanager/v1/users/project":
			version = jsonString(t, body, "version")
			_, _ = io.WriteString(w, `{"project":{"id":12,"name":"Demo"}}`)
		case recordPath:
			if got := jsonString(t, body, "client"); got != "" {
				t.Fatal("shape")
			}
			var rec struct {
				Client struct {
					Version string `json:"version"`
				} `json:"client"`
			}
			if err := json.Unmarshal(body, &rec); err != nil {
				t.Fatal(err)
			}
			version = rec.Client.Version
			_, _ = io.WriteString(w, `{"optResult":0}`)
		default:
			t.Fatal("path")
		}
	}))
	defer srv.Close()
	opt, out := pluginOpt(t, srv)
	err := Run(context.Background(), []string{"space", "--remote", "https://example.test/repo", "--space-version", "4.0.0", "8", "3"}, opt)
	if err != nil || version != "4.0.0" || !strings.Contains(out.String(), "生成代码采纳行数 8\n") {
		t.Fatal(err)
	}
}

func TestSpaceReleaseFailureUsesDefault(t *testing.T) {
	var version string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/api/acbackend/ideservice/v1/ide-releases":
			http.Error(w, "no", http.StatusInternalServerError)
		case "/api/acbackend/usermanager/v1/users/project":
			version = jsonString(t, body, "version")
			_, _ = io.WriteString(w, `{"project":{"id":1,"name":"P"}}`)
		case recordPath:
			var rec struct {
				Client struct {
					Version string `json:"version"`
				} `json:"client"`
			}
			if json.Unmarshal(body, &rec) != nil || rec.Client.Version != "1.5.1" {
				t.Fatal("version")
			}
			_, _ = io.WriteString(w, `{"code":0}`)
		default:
			t.Fatal("path")
		}
	}))
	defer srv.Close()
	opt, _ := pluginOpt(t, srv)
	if err := Run(context.Background(), []string{"space", "--remote", "https://example.test/repo", "8", "3"}, opt); err != nil || version != "1.5.1" {
		t.Fatal(err)
	}
}

func TestSpaceProjectFailureStillReports(t *testing.T) {
	var records int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/api/acbackend/usermanager/v1/users/project":
			http.Error(w, "no", http.StatusBadGateway)
		case recordPath:
			records++
			if r.Header.Get("projectId") != "0" || !strings.Contains(string(body), `"projectId":0`) || !strings.Contains(string(body), `"projectName":""`) {
				t.Fatal("project")
			}
			_, _ = io.WriteString(w, `{"code":0}`)
		default:
			t.Fatal("path")
		}
	}))
	defer srv.Close()
	opt, out := pluginOpt(t, srv)
	err := Run(context.Background(), []string{"space", "--remote", "https://example.test/repo", "--space-version", "1.5.1", "8", "3"}, opt)
	if err != nil || records != 4 || !strings.Contains(out.String(), "生成代码采纳行数 8\n") {
		t.Fatal(err)
	}
}

func TestSpaceStopsOnFailure(t *testing.T) {
	var records int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/acbackend/usermanager/v1/users/project":
			_, _ = io.WriteString(w, `{"project":{"id":12,"name":"Demo"}}`)
		case recordPath:
			records++
			if records == 1 {
				_, _ = io.WriteString(w, `{"code":0}`)
				return
			}
			http.Error(w, "no", http.StatusBadGateway)
		default:
			t.Fatal("path")
		}
	}))
	defer srv.Close()
	opt, out := pluginOpt(t, srv)
	err := Run(context.Background(), []string{"space", "--remote", "https://example.test/repo", "--space-version", "1.5.1", "8", "3"}, opt)
	if err != errReport || records != 2 || out.Len() != 0 {
		t.Fatal("stop")
	}
}

func TestSpaceMissingResultStops(t *testing.T) {
	var records int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/acbackend/usermanager/v1/users/project":
			_, _ = io.WriteString(w, `{"project":{"id":12,"name":"Demo"}}`)
		case recordPath:
			records++
			_, _ = io.WriteString(w, `{}`)
		default:
			t.Fatal("path")
		}
	}))
	defer srv.Close()
	opt, out := pluginOpt(t, srv)
	err := Run(context.Background(), []string{"space", "--remote", "https://example.test/repo", "--space-version", "1.5.1", "8", "3"}, opt)
	if err != errReport || records != 1 || out.Len() != 0 {
		t.Fatal("empty")
	}
}

func TestSpaceUnauthorizedDoesNotLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/acbackend/usermanager/v1/users/project" {
			_, _ = io.WriteString(w, `{"project":{"id":12,"name":"Demo"}}`)
			return
		}
		http.Error(w, "no", http.StatusUnauthorized)
	}))
	defer srv.Close()
	opt, out := pluginOpt(t, srv)
	err := Run(context.Background(), []string{"space", "--remote", "https://example.test/repo", "--space-version", "1.5.1", "8", "3"}, opt)
	if err != errReport || out.Len() != 0 {
		t.Fatal("login")
	}
}

func TestSpaceZeroSkipsNetwork(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("sent a request")
		return nil, nil
	})}
	var out strings.Builder
	err := Run(context.Background(), []string{"space", "0", "9"}, Options{
		Dir:    t.TempDir(),
		Stdout: &out,
		Stderr: io.Discard,
		Env:    []string{},
		Client: client,
		Open:   func(string) error { t.Fatal("login"); return nil },
	})
	if err != nil || out.String() != "生成代码采纳行数 0\n新增代码有效总行 9\n" {
		t.Fatal(err)
	}
}

func assertSpaceRecords(t *testing.T, records []map[string]any, version string, projectID int64, projectName, remote string) {
	t.Helper()
	want := []struct {
		key   string
		count string
		req   bool
	}{
		{"ide_agent_gen_code_line", "8", true},
		{"ide_agent_gen_code_count", "1", false},
		{"ide_agent_accepted_code_line", "8", true},
		{"ide_agent_accepted_code_count", "1", false},
	}
	if len(records) != len(want) {
		t.Fatal("count")
	}
	var reqs []string
	for i, item := range want {
		rec := records[i]
		if num, _ := rec["userid"].(json.Number); num.String() != "7" {
			t.Fatal("userid")
		}
		if num, _ := rec["projectId"].(json.Number); num.String() != json.Number(intString(projectID)).String() && num.String() != intString(projectID) {
			t.Fatal("projectId")
		}
		if rec["app"] != "codefree-ide" || rec["service"] != "ide_agent" || rec["time"] != "2026-01-02 05:03:04.008" {
			t.Fatal("record")
		}
		client, _ := rec["client"].(map[string]any)
		if client["type"] != spaceClientType() || client["version"] != version || client["gitUrl"] != remote || client["projectName"] != projectName {
			t.Fatal("client")
		}
		urls, _ := client["gitUrls"].([]any)
		if len(urls) != 1 || urls[0] != remote {
			t.Fatal("git")
		}
		activities, _ := rec["activities"].([]any)
		if len(activities) != 1 {
			t.Fatal("activities")
		}
		act, _ := activities[0].(map[string]any)
		if act["key"] != item.key || act["subKey"] != "Unknown" {
			t.Fatal("activity")
		}
		value, _ := act["value"].(map[string]any)
		count, _ := value["count"].(json.Number)
		if count.String() != item.count {
			t.Fatal("lines")
		}
		req, ok := rec["reqId"].(string)
		if item.req {
			if !ok || req == "" {
				t.Fatal("reqId")
			}
			reqs = append(reqs, req)
		} else if ok {
			t.Fatal("reqId")
		}
	}
	if len(reqs) != 2 || reqs[0] == reqs[1] {
		t.Fatal("reqIds")
	}
}

func jsonString(t *testing.T, body []byte, key string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	s, _ := m[key].(string)
	return s
}

func intString(n int64) string {
	return fmtInt(n)
}
