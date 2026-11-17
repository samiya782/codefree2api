package srd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const desktopUserKey = "srd-copilot.updateUserCache"

// stateCatalog lists the desktop state databases for one client. Tests replace it.
var stateCatalog = defaultStates

type stateDB struct {
	Path string
	Key  string
}

func defaultStates(kind Kind) []stateDB {
	var name, key string
	switch kind {
	case KindSpace:
		name = "CodeFree"
		key = "srdcloud.codefree-ide"
	case KindPlugin:
		name = "Code"
		key = "srdcloud.srd-copilot-vscode"
	default:
		return nil
	}
	var out []stateDB
	for _, root := range roamingRoots() {
		out = append(out, stateDB{
			Path: filepath.Join(root, name, "User", "globalStorage", "state.vscdb"),
			Key:  key,
		})
	}
	return out
}

func roamingRoots() []string {
	if app := os.Getenv("APPDATA"); app != "" && !strings.Contains(app, "/mnt/") {
		if fi, err := os.Stat(app); err == nil && fi.IsDir() {
			return []string{app}
		}
	}
	entries, err := os.ReadDir("/mnt/c/Users")
	if err != nil {
		return nil
	}
	skip := map[string]bool{
		"Public": true, "Default": true, "Default User": true,
		"All Users": true, "WsiAccount": true,
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || skip[e.Name()] || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		root := filepath.Join("/mnt/c/Users", e.Name(), "AppData", "Roaming")
		if fi, err := os.Stat(root); err == nil && fi.IsDir() {
			out = append(out, root)
		}
	}
	return out
}

// LoadDesktop reads a still-valid login from the installed client.
// The client's refresh token is not returned.
func LoadDesktop(kind Kind, now time.Time) *Session {
	var best *Session
	for _, db := range stateCatalog(kind) {
		raw, ok, err := readItem(db.Path, db.Key)
		if err != nil || !ok {
			continue
		}
		sess := sessionFromDesktop(raw, now)
		if sess == nil {
			continue
		}
		if best == nil || sess.CFExpiresAt > best.CFExpiresAt {
			best = sess
		}
	}
	return best
}

func sessionFromDesktop(raw []byte, now time.Time) *Session {
	var doc map[string]json.RawMessage
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	blob, ok := doc[desktopUserKey]
	if !ok || len(blob) == 0 || string(blob) == "null" {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(blob))
	dec.UseNumber()
	var user map[string]any
	if dec.Decode(&user) != nil {
		return nil
	}
	sess := &Session{
		UserID:      jsonString(user["userId"]),
		SessionID:   jsonString(user["sessionId"]),
		CFToken:     jsonString(user["cfToken"]),
		CFExpiresAt: jsonInt(user["cfTokenExpiresAt"]),
	}
	if !sess.usable(now) {
		return nil
	}
	sess.RefreshToken = ""
	sess.RefreshExpiresAt = 0
	return sess
}

func jsonString(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatInt(int64(t), 10)
	default:
		return ""
	}
}

func jsonInt(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case json.Number:
		n, err := t.Int64()
		if err != nil {
			f, ferr := t.Float64()
			if ferr != nil {
				return 0
			}
			return int64(f)
		}
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		return n
	default:
		return 0
	}
}
