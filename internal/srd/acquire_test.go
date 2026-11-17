package srd

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func useConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", filepath.Join(dir, "Roaming"))
}

func TestAcquireUsesDesktopWithoutRefresh(t *testing.T) {
	useConfig(t)
	old := stateCatalog
	stateCatalog = func(Kind) []stateDB {
		return []stateDB{{Path: "testdata/state.vscdb", Key: "srdcloud.codefree-ide"}}
	}
	t.Cleanup(func() { stateCatalog = old })
	sess, own, err := Acquire(context.Background(), AcquireOptions{
		App:  Space(),
		Now:  func() time.Time { return time.UnixMilli(1_700_000_000_000) },
		Open: func(string) error { t.Fatal("page"); return nil },
	})
	if err != nil || own || sess == nil || sess.UserID != "42" || sess.RefreshToken != "" {
		t.Fatal("desktop")
	}
}

func TestAcquireRefreshesOwnAndSkipsDesktop(t *testing.T) {
	useConfig(t)
	old := stateCatalog
	stateCatalog = func(Kind) []stateDB {
		return []stateDB{{Path: "testdata/state.vscdb", Key: "srdcloud.codefree-ide"}}
	}
	t.Cleanup(func() { stateCatalog = old })
	app := App{Kind: KindSpace, ClientID: "app-client", ClientSecret: "shipped"}
	prev := &Session{
		UserID:       "7",
		SessionID:    "sess",
		CFToken:      "old",
		RefreshToken: "old-refresh",
		CFExpiresAt:  1,
	}
	if err := SaveOwn(KindSpace, prev); err != nil {
		t.Fatal("save")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != tokenPath || r.Method != http.MethodPost {
			t.Fatal("path")
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal("form")
		}
		if r.PostForm.Get("grant_type") != "refresh_x_cf_token" || r.PostForm.Get("client_secret") != "shipped" || r.PostForm.Get("x_cf_refresh_token") != "old-refresh" {
			t.Fatal("refresh form")
		}
		_, _ = io.WriteString(w, `{"x_cf_token":"new-cf","expires_in":600,"x_cf_refresh_token":"new-refresh","x_cf_refresh_token_expires_in":86400}`)
	}))
	defer srv.Close()
	app.Host = srv.URL
	sess, own, err := Acquire(context.Background(), AcquireOptions{
		App:    app,
		HTTP:   srv.Client(),
		Now:    func() time.Time { return time.UnixMilli(1_700_000_000_000) },
		Open:   func(string) error { t.Fatal("page"); return nil },
		Stderr: io.Discard,
	})
	if err != nil || !own || sess == nil || sess.UserID != "7" || sess.CFToken != "new-cf" || sess.RefreshToken != "new-refresh" {
		t.Fatal("refreshed")
	}
	loaded, err := LoadOwn(KindSpace)
	if err != nil || loaded == nil || loaded.CFToken != "new-cf" {
		t.Fatal("stored")
	}
}

func TestDeleteOwn(t *testing.T) {
	useConfig(t)
	if err := SaveOwn(KindPlugin, &Session{UserID: "1", CFToken: "c"}); err != nil {
		t.Fatal("save")
	}
	if err := DeleteOwn(KindPlugin); err != nil {
		t.Fatal("delete")
	}
	got, err := LoadOwn(KindPlugin)
	if err != nil || got != nil {
		t.Fatal("gone")
	}
}
