package lines

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	refreshKeyLabel = "codefree-o-refresh-token-local-encryption-key-v1"
	hmacSecretB64   = "HJhzHSFDctmDwSiXLdiidecX8p1uBmG2HHbTf5rD9n4="
	prodAppID       = "2542334461226y3rzqmr"
	prodSecret      = "254233446135xuMOKaXTmgNMby4f42Ae7DRhEBAe"
	devAppID        = "2538b593b435ordz03y5"
	devSecret       = "2538b593b438oyhwJqAfwlLD6Piz3LVTdpBTCTH5"
	testAppID       = "253b1906b368ack1rfoh"
	testSecret      = "253b1906b377WdoIMBvACC7iVemSnNXTKfOshlAb"
	tokenPath       = "/api/usercenterbackend/oauth/access_token"
	recordPath      = "/api/acbackend/statserver/v1/activity_record"
)

// clientAppID and clientSecret match the values embedded in codefree-o 1.10.1.
// The host string is classified the same way: "dev" wins over "test".
func clientAppID(baseURL string) string {
	switch {
	case strings.Contains(baseURL, "dev"):
		return devAppID
	case strings.Contains(baseURL, "test"):
		return testAppID
	default:
		return prodAppID
	}
}

func clientSecret(baseURL string, env []string) string {
	if v := strings.TrimSpace(envGet(env, "CODEFREE_CLIENT_SECRET")); v != "" {
		return v
	}
	switch {
	case strings.Contains(baseURL, "dev"):
		return devSecret
	case strings.Contains(baseURL, "test"):
		return testSecret
	default:
		return prodSecret
	}
}

type login struct {
	mode      string
	userID    string
	baseURL   string
	projectID string
	appID     string
	version   string
	cfToken   string
	client    *http.Client
	now       func() time.Time
	env       []string
	authPath  string
}

func openLogin(ctx context.Context, env []string, client *http.Client, now func() time.Time, version string) (*login, error) {
	if now == nil {
		now = time.Now
	}
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	cfg, err := resolveEnvAuth(env)
	if err != nil {
		return nil, err
	}
	if cfg != nil {
		if _, err := parseCount(cfg.userID); err != nil {
			return nil, errNoLogin
		}
		return &login{
			mode:      "environment",
			userID:    cfg.userID,
			baseURL:   strings.TrimRight(cfg.baseURL, "/"),
			projectID: cfg.projectID,
			appID:     clientAppID(cfg.baseURL),
			version:   version,
			client:    client,
			now:       now,
			env:       env,
		}, nil
	}
	path, rec, err := readAuthFile(env)
	if err != nil {
		return nil, err
	}
	if _, err := parseCount(rec.UserID); err != nil {
		return nil, errNoLogin
	}
	l := &login{
		mode:      "oauth",
		userID:    rec.UserID,
		baseURL:   strings.TrimRight(rec.BaseURLSnapshot, "/"),
		projectID: "0",
		appID:     clientAppID(rec.BaseURLSnapshot),
		version:   version,
		client:    client,
		now:       now,
		env:       env,
		authPath:  path,
	}
	if err := l.refresh(ctx, rec.refresh); err != nil {
		return nil, err
	}
	return l, nil
}

type envAuth struct {
	accessToken string
	userID      string
	projectID   string
	baseURL     string
}

func resolveEnvAuth(env []string) (*envAuth, error) {
	keys := []string{"SRDCLOUD_CF_ACCESS_TOKEN", "SRDCLOUD_USERID", "SRDCLOUD_PROJECTID", "SRDCLOUD_BASE_URL"}
	vals := make([]string, len(keys))
	missing := 0
	for i, key := range keys {
		vals[i] = strings.TrimSpace(envGet(env, key))
		if vals[i] == "" {
			missing++
		}
	}
	if missing == len(keys) {
		return nil, nil
	}
	if missing > 0 {
		return nil, errNoLogin
	}
	return &envAuth{accessToken: vals[0], userID: vals[1], projectID: vals[2], baseURL: vals[3]}, nil
}

type authFile struct {
	Version                 int    `json:"version"`
	EncryptedRefreshCfToken string `json:"encryptedRefreshCfToken"`
	RefreshCfToken          string `json:"refreshCfToken,omitempty"`
	RefreshCfTokenExpiresAt int64  `json:"refreshCfTokenExpiresAt"`
	UserID                  string `json:"userId"`
	BaseURLSnapshot         string `json:"baseUrlSnapshot"`
	refresh                 string
}

func readAuthFile(env []string) (string, authFile, error) {
	for _, path := range authCandidates(env) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var rec authFile
		if err := json.Unmarshal(data, &rec); err != nil || rec.Version != 2 {
			continue
		}
		switch {
		case rec.EncryptedRefreshCfToken != "":
			plain, err := decryptRefresh(rec.EncryptedRefreshCfToken)
			if err != nil {
				return "", authFile{}, errNoLogin
			}
			rec.refresh = plain
		case rec.RefreshCfToken != "":
			rec.refresh = rec.RefreshCfToken
		default:
			continue
		}
		if rec.UserID == "" || rec.BaseURLSnapshot == "" || rec.refresh == "" {
			continue
		}
		return path, rec, nil
	}
	return "", authFile{}, errNoLogin
}

func authCandidates(env []string) []string {
	if path := strings.TrimSpace(envGet(env, "OPENCODE_CODEFREE_AUTH_FILE")); path != "" {
		return []string{path}
	}
	home := strings.TrimSpace(envGet(env, "OPENCODE_TEST_HOME"))
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return nil
		}
	}
	return []string{
		filepath.Join(home, ".codefree-o", ".local", "share", "codefree.json"),
		filepath.Join(home, ".local", "share", "opencode", "codefree.json"),
		filepath.Join(home, ".codefree", "common", "agent", "core", ".codefree-o", "codefree.json"),
	}
}

func (l *login) refresh(ctx context.Context, refreshToken string) error {
	unlock, err := lockAuth(filepath.Dir(l.authPath))
	if err != nil {
		return errNoLogin
	}
	defer unlock()
	// Another run may have rotated the refresh token while we waited.
	_, rec, err := readAuthFile(l.env)
	if err != nil {
		return err
	}
	if rec.refresh != "" {
		refreshToken = rec.refresh
	}
	body := refreshForm(l.appID, refreshToken, clientSecret(l.baseURL, l.env))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.baseURL+tokenPath, strings.NewReader(body))
	if err != nil {
		return errRefresh
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "opencode/"+l.version)
	resp, err := l.client.Do(req)
	if err != nil {
		return errRefresh
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return errRefresh
	}
	if resp.StatusCode != http.StatusOK {
		return errRefresh
	}
	next, err := parseRefresh(payload)
	if err != nil {
		return errRefresh
	}
	if err := writeAuthFile(l.authPath, authFile{
		Version:                 2,
		EncryptedRefreshCfToken: encryptRefresh(next.refresh),
		RefreshCfTokenExpiresAt: l.now().Add(time.Duration(next.refreshExpires) * time.Second).UnixMilli(),
		UserID:                  l.userID,
		BaseURLSnapshot:         l.baseURL,
	}); err != nil {
		return errRefresh
	}
	l.cfToken = next.access
	return nil
}

type refreshed struct {
	access         string
	refresh        string
	refreshExpires int64
}

func parseRefresh(payload []byte) (refreshed, error) {
	var raw map[string]any
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return refreshed{}, err
	}
	access := firstString(raw, "x_cf_token", "cf_token")
	if access == "" && stringField(raw, "token_type") == "cf_token" {
		access = stringField(raw, "access_token")
	}
	refresh := firstString(raw, "x_cf_refresh_token", "refresh_token", "refresh_cf_token", "cf_token_refresh_token")
	if access == "" || refresh == "" {
		return refreshed{}, fmt.Errorf("missing token")
	}
	exp := firstInt(raw, "x_cf_refresh_token_expires_in", "refresh_token_expires_in", "refresh_cf_token_expires_in")
	return refreshed{access: access, refresh: refresh, refreshExpires: exp}, nil
}

func firstString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if v := stringField(raw, key); v != "" {
			return v
		}
	}
	return ""
}

func stringField(raw map[string]any, key string) string {
	v, ok := raw[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func firstInt(raw map[string]any, keys ...string) int64 {
	for _, key := range keys {
		v, ok := raw[key]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case json.Number:
			n, err := t.Int64()
			if err == nil {
				return n
			}
		case string:
			n, err := strconvParse(t)
			if err == nil {
				return n
			}
		}
	}
	return 0
}

func strconvParse(s string) (int64, error) {
	return parseCount(strings.TrimSpace(s))
}

func refreshForm(appID, refresh, secret string) string {
	return strings.Join([]string{
		"grant_type=refresh_x_cf_token",
		"client_id=" + url.QueryEscape(appID),
		"x_cf_refresh_token=" + url.QueryEscape(refresh),
		"client_secret=" + url.QueryEscape(secret),
	}, "&")
}

func writeAuthFile(path string, rec authFile) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".codefree-auth-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func refreshKey() []byte {
	sum := sha256.Sum256([]byte(refreshKeyLabel))
	return sum[:]
}

func encryptRefresh(plain string) string {
	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		panic(err)
	}
	block, err := aes.NewCipher(refreshKey())
	if err != nil {
		panic(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	sealed := gcm.Seal(nil, iv, []byte(plain), nil)
	tag := sealed[len(sealed)-gcm.Overhead():]
	ct := sealed[:len(sealed)-gcm.Overhead()]
	return strings.Join([]string{
		"v1",
		base64.StdEncoding.EncodeToString(iv),
		base64.StdEncoding.EncodeToString(ct),
		base64.StdEncoding.EncodeToString(tag),
	}, ":")
}

func decryptRefresh(packed string) (string, error) {
	parts := strings.Split(packed, ":")
	if len(parts) != 4 || parts[0] != "v1" {
		return "", fmt.Errorf("bad packing")
	}
	iv, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", err
	}
	ct, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return "", err
	}
	tag, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(refreshKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	plain, err := gcm.Open(nil, iv, append(ct, tag...), nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func sign(method, appID, timestamp, nonce string) (string, error) {
	key, err := base64.StdEncoding.DecodeString(hmacSecretB64)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = io.WriteString(mac, strings.ToUpper(method)+"\n"+appID+"\n"+timestamp+"\n"+nonce)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

func envGet(env []string, key string) string {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			return env[i][len(prefix):]
		}
	}
	return ""
}
