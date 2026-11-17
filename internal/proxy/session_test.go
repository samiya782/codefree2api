package proxy

import (
	"path/filepath"
	"testing"

	"codefree2api/internal/srd"
)

func TestClearDropsOwnSessionOnly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", filepath.Join(dir, "Roaming"))
	app := srd.Plugin()
	if err := srd.SaveOwn(app.Kind, &srd.Session{UserID: "1", CFToken: "c"}); err != nil {
		t.Fatal("save")
	}
	kept := &remote{App: app}
	kept.clear()
	if got, _ := srd.LoadOwn(app.Kind); got == nil || !kept.force {
		t.Fatal("desktop source")
	}
	own := &remote{App: app, own: true}
	own.clear()
	if got, _ := srd.LoadOwn(app.Kind); got != nil || !own.force {
		t.Fatal("own source")
	}
}
