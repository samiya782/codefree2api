package srd

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestLoginExchange(t *testing.T) {
	var sawBearer bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Cf-Signature") != "" {
			t.Fatal("signed")
		}
		switch r.URL.Path {
		case tokenPath:
			if r.URL.Query().Get("client_secret") != "" || r.URL.Query().Get("code_verifier") == "" {
				t.Fatal("token query")
			}
			if r.URL.Query().Get("grant_type") != "authorization_code" {
				t.Fatal("grant")
			}
			_, _ = io.WriteString(w, `{"uid":7,"ori_session_id":"sess","access_token":"atk"}`)
		case userPath:
			sawBearer = r.Header.Get("Authorization") == "Bearer atk" && r.Header.Get("userId") == "7" && r.Header.Get("sessionId") == "sess"
			_, _ = io.WriteString(w, `{"x_cf_token":"cft","x_cf_refresh_token":"rft"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	var stderr strings.Builder
	sess, err := Login(context.Background(), LoginOptions{
		App:    App{Kind: KindPlugin, Host: srv.URL, ClientID: "app-client", Signing: "dGVzdC1zZWNyZXQ="},
		HTTP:   srv.Client(),
		Stderr: &stderr,
		Open: func(raw string) error {
			if strings.Contains(raw, "client_secret") || !strings.Contains(raw, "client_id=") || !strings.Contains(raw, "code_challenge_method=S256") {
				t.Fatal("authorize")
			}
			if !strings.Contains(raw, "from%3Dvscode") && !strings.Contains(raw, "from=vscode") {
				page, err := url.Parse(raw)
				if err != nil {
					t.Fatal("authorize url")
				}
				state, err := base64.StdEncoding.DecodeString(page.Query().Get("state"))
				if err != nil || !strings.Contains(string(state), "from=vscode") {
					t.Fatal("state")
				}
			}
			page, err := url.Parse(raw)
			if err != nil {
				t.Fatal("authorize url")
			}
			state, err := base64.StdEncoding.DecodeString(page.Query().Get("state"))
			if err != nil {
				t.Fatal("state")
			}
			callback := string(state)
			sep := "&"
			if !strings.Contains(callback, "?") {
				sep = "?"
			}
			resp, err := http.Get(callback + sep + "code=abc")
			if err != nil {
				t.Fatal("callback")
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "登录完成，可以关闭这个页面。") {
				t.Fatal("page")
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if sess.UserID != "7" || sess.SessionID != "sess" || sess.CFToken != "cft" || sess.RefreshToken != "rft" || !sawBearer {
		t.Fatal("session")
	}
	if stderr.Len() != 0 {
		t.Fatal("printed")
	}
}

func TestLoginTokenRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != tokenPath {
			t.Fatal("path")
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "error=bad_verification_code&error_description=secret-value")
	}))
	defer srv.Close()
	_, err := Login(context.Background(), LoginOptions{
		App:  App{Kind: KindSpace, Host: srv.URL, ClientID: "app-client"},
		HTTP: srv.Client(),
		Open: func(raw string) error {
			page, err := url.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			state, err := base64.StdEncoding.DecodeString(page.Query().Get("state"))
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.Get(string(state) + "&code=abc")
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			return nil
		},
	})
	if !errors.Is(err, ErrLogin) {
		t.Fatal(err)
	}
	if err.Error() != "登录失败（令牌交换 bad_verification_code）" || strings.Contains(err.Error(), "secret") {
		t.Fatal(err)
	}
}

func TestTokenForm(t *testing.T) {
	id, sid, access := tokenFields([]byte("uid=9&session_id=s&access_token=a"))
	if id != "9" || sid != "s" || access != "a" {
		t.Fatal("form")
	}
}
