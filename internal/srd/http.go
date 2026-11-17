package srd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const bodyLimit = 2 << 20

// Session is one client's 研发云 identity. login saves it for later commands.
// It is never printed.
type Session struct {
	UserID           string
	SessionID        string
	CFToken          string
	RefreshToken     string
	CFExpiresAt      int64
	RefreshExpiresAt int64
}

// Call is one HTTP request. Signed is forced off for the legacy unsigned paths.
type Call struct {
	Method    string
	Path      string
	Query     url.Values
	Body      []byte
	Header    http.Header
	ProjectID string
	GitURLs   []string
	Unsigned  bool
}

// Caller talks to one App. A nil HTTP client uses http.DefaultClient.
type Caller struct {
	App     App
	HTTP    *http.Client
	Now     func() time.Time
	Session *Session
}

func (c *Caller) now() time.Time {
	if c != nil && c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Caller) http() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// Do performs call and returns the status and body. A non-2xx status is not
// an error. Transport failures are errors and do not include the response body.
func (c *Caller) Do(ctx context.Context, call Call) (int, []byte, error) {
	resp, err := c.Open(ctx, call)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, bodyLimit))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, payload, nil
}

// Open performs call and returns the response. The caller closes the body.
func (c *Caller) Open(ctx context.Context, call Call) (*http.Response, error) {
	req, err := c.request(ctx, call)
	if err != nil {
		return nil, err
	}
	return c.http().Do(req)
}

func (c *Caller) request(ctx context.Context, call Call) (*http.Request, error) {
	method := strings.ToUpper(call.Method)
	if method == "" {
		method = http.MethodGet
	}
	path := call.Path
	if i := strings.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	endpoint := strings.TrimRight(c.App.Host, "/") + path
	if q := call.Query.Encode(); q != "" {
		endpoint += "?" + q
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(call.Body))
	if err != nil {
		return nil, err
	}
	for key, values := range call.Header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "codefree2api")
	}
	if len(call.Body) > 0 && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if !call.Unsigned && !legacyPath(path) {
		headers, err := AuthHeaders(c.App, c.Session, method, c.now(), call.ProjectID)
		if err != nil {
			return nil, err
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		if c.Session != nil && c.Session.UserID != "" {
			req.Header.Set("userId", c.Session.UserID)
		}
		if len(call.GitURLs) > 0 {
			req.Header.Set("gitUrls", strings.Join(call.GitURLs, ","))
		}
	}
	return req, nil
}

func legacyPath(path string) bool {
	switch path {
	case "/login/oauth/access_token",
		"/api/usercenterbackend/user",
		"/api/usercenterbackend/users/v1/get-user-front":
		return true
	}
	return strings.HasPrefix(path, "/api/usercenterbackend/users/v1/get-user-front/")
}

// Marshal encodes JSON without HTML escaping so git URLs stay intact.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSpace(buf.Bytes()), nil
}
