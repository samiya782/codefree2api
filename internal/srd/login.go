package srd

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	authorizePath = "/login/oauth/authorize"
	tokenPath     = "/login/oauth/access_token"
	redirectPath  = "/login/oauth-srdcloud-redirect"
	userPath      = "/api/usercenterbackend/user"
	pkceChars     = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~"
)

// ErrLogin is returned when the page login does not yield a session.
// It carries no token and no URL.
var ErrLogin = errors.New("登录失败")

// LoginOptions opens one client's 研发云 login page and waits for the callback.
type LoginOptions struct {
	App    App
	HTTP   *http.Client
	Now    func() time.Time
	Open   func(string) error
	Stderr io.Writer
}

// Login listens on 127.0.0.1, opens the authorize page, and exchanges the
// code. The session stays in memory. client_secret is not sent.
func Login(ctx context.Context, opt LoginOptions) (*Session, error) {
	if opt.Open == nil {
		opt.Open = OpenBrowser
	}
	if opt.Stderr == nil {
		opt.Stderr = io.Discard
	}
	verifier, err := pkceVerifier(64)
	if err != nil {
		return nil, ErrLogin
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, ErrLogin
	}
	defer ln.Close()
	nonce, err := pkceVerifier(32)
	if err != nil {
		return nil, ErrLogin
	}
	callback := fmt.Sprintf("http://%s/did-authenticate?nonce=%s&from=%s", ln.Addr().String(), url.QueryEscape(nonce), url.QueryEscape(fromValue(opt.App.Kind)))
	codeCh := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/did-authenticate", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("nonce"); got != "" && got != nonce {
			http.Error(w, "nonce", http.StatusBadRequest)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "code", http.StatusBadRequest)
			return
		}
		select {
		case codeCh <- code:
		default:
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!DOCTYPE html><meta charset=\"utf-8\"><title>登录完成</title><p>登录完成，可以关闭这个页面。</p>")
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer func() {
		shut, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()

	authURL := authorizeURL(opt.App, callback, verifier)
	if err := opt.Open(authURL); err != nil {
		fmt.Fprintf(opt.Stderr, "登录页：%s\n", authURL)
	}
	var code string
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case code = <-codeCh:
	}
	return exchange(ctx, opt, verifier, code)
}

func fromValue(kind Kind) string {
	if kind == KindPlugin {
		return "vscode"
	}
	return "ide"
}

func authorizeURL(app App, callback, verifier string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", app.ClientID)
	q.Set("redirect_uri", strings.TrimRight(app.Host, "/")+redirectPath)
	q.Set("state", base64.StdEncoding.EncodeToString([]byte(callback)))
	q.Set("code_challenge", challenge(verifier))
	q.Set("code_challenge_method", "S256")
	return strings.TrimRight(app.Host, "/") + authorizePath + "?" + q.Encode()
}

func exchange(ctx context.Context, opt LoginOptions, verifier, code string) (*Session, error) {
	redirect := strings.TrimRight(opt.App.Host, "/") + redirectPath
	q := url.Values{}
	q.Set("grant_type", "authorization_code")
	q.Set("client_id", opt.App.ClientID)
	q.Set("code", code)
	q.Set("redirect_uri", redirect)
	q.Set("code_verifier", verifier)
	caller := &Caller{App: opt.App, HTTP: opt.HTTP, Now: opt.Now}
	status, body, err := caller.Do(ctx, Call{Method: http.MethodGet, Path: tokenPath, Query: q, Unsigned: true})
	if err != nil || status != http.StatusOK {
		return nil, ErrLogin
	}
	userID, sessionID, access := tokenFields(body)
	if userID == "" || access == "" {
		return nil, ErrLogin
	}
	now := time.Now()
	if opt.Now != nil {
		now = opt.Now()
	}
	tokenBody := body
	header := make(http.Header)
	header.Set("Authorization", "Bearer "+access)
	header.Set("userId", userID)
	if sessionID != "" {
		header.Set("sessionId", sessionID)
	}
	status, body, err = caller.Do(ctx, Call{Method: http.MethodGet, Path: userPath, Header: header, Unsigned: true})
	if err != nil || status != http.StatusOK {
		return nil, ErrLogin
	}
	cf := firstField(body, "x_cf_token")
	if cf == "" {
		cf = firstField(body, "access_token")
	}
	if cf == "" {
		return nil, ErrLogin
	}
	sess := &Session{UserID: userID, SessionID: sessionID, CFToken: cf}
	takeRefresh(sess, tokenBody, now)
	takeRefresh(sess, body, now)
	takeCFExpiry(sess, body, now)
	takeCFExpiry(sess, tokenBody, now)
	return sess, nil
}

func tokenFields(body []byte) (userID, sessionID, access string) {
	userID = firstField(body, "uid")
	sessionID = firstField(body, "ori_session_id")
	if sessionID == "" {
		sessionID = firstField(body, "session_id")
	}
	access = firstField(body, "access_token")
	return userID, sessionID, access
}

func firstField(body []byte, key string) string {
	body = bytesTrim(body)
	if len(body) == 0 {
		return ""
	}
	if body[0] == '{' || body[0] == '[' {
		dec := json.NewDecoder(strings.NewReader(string(body)))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			return ""
		}
		return walkField(v, key)
	}
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(values.Get(key))
}

func walkField(v any, key string) string {
	switch t := v.(type) {
	case map[string]any:
		if s := scalar(t[key]); s != "" {
			return s
		}
		for _, child := range t {
			if s := walkField(child, key); s != "" {
				return s
			}
		}
	case []any:
		for _, child := range t {
			if s := walkField(child, key); s != "" {
				return s
			}
		}
	}
	return ""
}

func scalar(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case json.Number:
		return t.String()
	case nil:
		return ""
	default:
		return ""
	}
}

func pkceVerifier(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := io.ReadFull(randReader{}, buf); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i, b := range buf {
		out[i] = pkceChars[int(b)%len(pkceChars)]
	}
	return string(out), nil
}

type randReader struct{}

func (randReader) Read(p []byte) (int, error) {
	return cryptoRand(p)
}

func challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func bytesTrim(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}
