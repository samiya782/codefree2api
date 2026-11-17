package srd

import (
	"bytes"
	"testing"
	"time"
)

func TestReadItemOverflow(t *testing.T) {
	raw, ok, err := readItem("testdata/state.vscdb", "srdcloud.codefree-ide")
	if err != nil || !ok {
		t.Fatal("item")
	}
	if !bytes.Contains(raw, []byte(`"userId": "42"`)) && !bytes.Contains(raw, []byte(`"userId":"42"`)) {
		t.Fatal("user")
	}
	if _, ok, err = readItem("testdata/state.vscdb", "missing"); err != nil || ok {
		t.Fatal("missing")
	}
	sess := sessionFromDesktop(raw, time.UnixMilli(1_700_000_000_000))
	if sess == nil || sess.UserID != "42" || sess.CFToken != "cf-token" || sess.SessionID != "sess-1" || sess.RefreshToken != "" {
		t.Fatal("session")
	}
	if sessionFromDesktop(raw, time.UnixMilli(2_000_000_000_000)) != nil {
		t.Fatal("expired")
	}
}
