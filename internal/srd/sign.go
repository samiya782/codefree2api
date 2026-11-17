package srd

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Sign is HMAC-SHA256 of METHOD, app id, unix seconds, and nonce, each on
// its own line. The signing secret is base64. A secret that is not base64
// is used as raw bytes.
func Sign(method, appID, secret, timestamp, nonce string) (string, error) {
	mac := hmac.New(sha256.New, signingKey(secret))
	_, _ = io.WriteString(mac, strings.ToUpper(method)+"\n"+appID+"\n"+timestamp+"\n"+nonce)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil)), nil
}

func signingKey(secret string) []byte {
	key, err := base64.StdEncoding.DecodeString(secret)
	if err != nil || len(key) == 0 {
		return []byte(secret)
	}
	return key
}

// Nonce matches the manual cf-token nonce: a millisecond prefix plus a UUID.
func Nonce(now time.Time) (string, error) {
	id, err := newID()
	if err != nil {
		return "", err
	}
	return "remote-cf-token-hmac-manual-" + strconv.FormatInt(now.UnixMilli(), 10) + "-" + id, nil
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// AuthHeaders are the signed headers shared by HTTP and the websocket
// context. userId is not included; callers add it only on HTTP.
func AuthHeaders(app App, sess *Session, method string, now time.Time, projectID string) (map[string]string, error) {
	if projectID == "" {
		projectID = "0"
	}
	timestamp := strconv.FormatInt(now.Unix(), 10)
	nonce, err := Nonce(now)
	if err != nil {
		return nil, err
	}
	signature, err := Sign(method, app.ClientID, app.Signing, timestamp, nonce)
	if err != nil {
		return nil, err
	}
	h := map[string]string{
		"X-Cf-AppId":     app.ClientID,
		"X-Cf-Timestamp": timestamp,
		"X-Cf-Nonce":     nonce,
		"X-Cf-Signature": signature,
		"projectId":      projectID,
	}
	if sess != nil && sess.CFToken != "" {
		h["X-Cf-Token"] = sess.CFToken
	}
	return h, nil
}
