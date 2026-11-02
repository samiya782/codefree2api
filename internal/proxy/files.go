package proxy

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func materializeFiles(dir string, files []FileRef) ([]FileRef, error) {
	if len(files) == 0 {
		return nil, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	out := make([]FileRef, 0, len(files))
	for i, f := range files {
		if strings.HasPrefix(f.URL, "data:") {
			mime, raw, ok := decodeDataURL(f.URL)
			if !ok {
				return nil, badRequest("messages", "unsupported file data URL")
			}
			if f.MIME == "" {
				f.MIME = mime
			}
			name := filepath.Base(f.Name)
			if name == "" || name == "." || name == string(filepath.Separator) {
				name = fmt.Sprintf("attach-%d%s", i, extForMIME(f.MIME))
			}
			path := filepath.Join(dir, fmt.Sprintf("%d-%s", time.Now().UnixNano(), name))
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				return nil, err
			}
			f.URL = "file://" + path
			f.Name = name
		}
		if f.MIME == "" {
			f.MIME = "application/octet-stream"
		}
		if strings.TrimSpace(f.URL) == "" {
			continue
		}
		out = append(out, f)
	}
	return out, nil
}

func decodeDataURL(s string) (string, []byte, bool) {
	if !strings.HasPrefix(s, "data:") {
		return "", nil, false
	}
	rest := strings.TrimPrefix(s, "data:")
	comma := strings.IndexByte(rest, ',')
	if comma < 0 {
		return "", nil, false
	}
	meta, payload := rest[:comma], rest[comma+1:]
	if !strings.Contains(strings.ToLower(meta), ";base64") {
		return "", nil, false
	}
	mime := meta
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = mime[:i]
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(payload)
		if err != nil {
			return "", nil, false
		}
	}
	return mime, raw, true
}

func extForMIME(mime string) string {
	switch strings.ToLower(mime) {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "application/pdf":
		return ".pdf"
	default:
		return ".bin"
	}
}
