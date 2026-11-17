package lines

import (
	"bytes"
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"codefree2api/internal/srd"
)

func clientApp(kind srd.Kind, base string) srd.App {
	app := srd.Plugin()
	if kind == srd.KindSpace {
		app = srd.Space()
	}
	if base != "" {
		app.Host = strings.TrimRight(base, "/")
	}
	return app
}

func ensureSession(ctx context.Context, opt Options, app srd.App) (*srd.Session, error) {
	sess, _, err := srd.Acquire(ctx, srd.AcquireOptions{
		App:     app,
		HTTP:    opt.Client,
		Now:     opt.Now,
		Open:    opt.Open,
		Stderr:  opt.Stderr,
		Session: opt.Session,
	})
	return sess, err
}

func callerFor(opt Options, app srd.App, sess *srd.Session) *srd.Caller {
	client := opt.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &srd.Caller{App: app, HTTP: client, Now: opt.Now, Session: sess}
}

func editorVersion(ctx context.Context, opt Options, flag string) (string, error) {
	if strings.TrimSpace(flag) != "" {
		return strings.TrimSpace(flag), nil
	}
	var (
		found string
		err   error
	)
	if opt.Editor != nil {
		found, err = opt.Editor(ctx)
	} else {
		found, err = lookupEditor(ctx)
	}
	if err != nil || strings.TrimSpace(found) == "" {
		return "", errNoEditorVersion
	}
	return strings.TrimSpace(found), nil
}

func lookupEditor(ctx context.Context) (string, error) {
	found, err := srd.EditorVersion(ctx)
	if err != nil || strings.TrimSpace(found) == "" {
		return "", errNoEditorVersion
	}
	return found, nil
}

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

func fmtInt(n int64) string { return strconv.FormatInt(n, 10) }
