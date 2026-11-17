package srd

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestSignVector(t *testing.T) {
	got, err := Sign("post", "app-1", "dGVzdC1zZWNyZXQ=", "1700000000", "nonce-1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "CDgzvd4TBkzVAnbRcAHYIdSV2jtcPyPKbcuuX5c6e0E=" {
		t.Fatal("vector")
	}
}

func TestSignRawSecret(t *testing.T) {
	mac := hmac.New(sha256.New, []byte("test-secret"))
	mac.Write([]byte("GET\napp\n1\nn"))
	want := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	got, err := Sign("get", "app", "test-secret", "1", "n")
	if err != nil || got != want {
		t.Fatal("raw")
	}
}

func TestNonceShape(t *testing.T) {
	now := time.UnixMilli(1700000000123)
	got, err := Nonce(now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "remote-cf-token-hmac-manual-1700000000123-") {
		t.Fatal("prefix")
	}
}

func TestPackageCredentials(t *testing.T) {
	plugin := Plugin()
	space := Space()
	if !strings.HasPrefix(plugin.Host, "https://") || !strings.HasPrefix(plugin.WS, "wss://") || plugin.Host != space.Host {
		t.Fatal("host")
	}
	if plugin.ClientID == "" || space.ClientID == "" || plugin.Signing == "" || space.Signing == "" || plugin.ClientSecret == "" || space.ClientSecret == "" {
		t.Fatal("missing")
	}
	if plugin.Signing == space.Signing || plugin.ClientID == space.ClientID || plugin.ClientSecret == space.ClientSecret {
		t.Fatal("shared")
	}
}
