package srd

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebSocketRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("hijack")
		}
		conn, rw, err := hj.Hijack()
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		key := r.Header.Get("Sec-WebSocket-Key")
		sum := sha1.Sum([]byte(key + wsGUID))
		accept := base64.StdEncoding.EncodeToString(sum[:])
		fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", accept)
		if err := rw.Flush(); err != nil {
			t.Fatal(err)
		}
		text := readClientText(t, rw.Reader)
		if text != channelStart+"ping"+channelEnd {
			t.Fatal("frame")
		}
		if err := writeServerText(rw.Writer, []byte(channelStart+"pong"+channelEnd)); err != nil {
			t.Fatal(err)
		}
		if err := rw.Flush(); err != nil {
			t.Fatal(err)
		}
	}))
	defer srv.Close()
	conn, err := Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/websocket/peerAppgw")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteChannel([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	got, err := conn.ReadChannel()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "pong" {
		t.Fatalf("got %q", got)
	}
}

func readClientText(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	fin, op, payload, err := readFrame(r)
	if err != nil || !fin || op != opText {
		t.Fatal("read")
	}
	return string(payload)
}

func writeServerText(w *bufio.Writer, payload []byte) error {
	return writeFrame(w, opText, payload, false)
}
