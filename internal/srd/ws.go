package srd

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	wsGUID       = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
	maxFrame     = 8 << 20
	opText       = 0x1
	opClose      = 0x8
	opPing       = 0x9
	opPong       = 0xA
	channelStart = "<WBChannel>"
	channelEnd   = "</WBChannel>"
)

// Conn is a client websocket. Outgoing frames are masked. Incoming frames
// are accepted masked or plain.
type Conn struct {
	conn    net.Conn
	r       *bufio.Reader
	mu      sync.Mutex
	done    chan struct{}
	pending string
}

// Dial opens a websocket. wsURL is ws:// or wss://. The context closes the
// connection when it is canceled.
func Dial(ctx context.Context, wsURL string) (*Conn, error) {
	u, err := url.Parse(wsURL)
	if err != nil {
		return nil, err
	}
	host := u.Host
	if !strings.Contains(host, ":") {
		if u.Scheme == "wss" {
			host += ":443"
		} else {
			host += ":80"
		}
	}
	dialer := net.Dialer{}
	raw, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, err
	}
	conn := raw
	if u.Scheme == "wss" {
		tlsConn, err := tlsClient(ctx, raw, u.Hostname())
		if err != nil {
			raw.Close()
			return nil, err
		}
		conn = tlsConn
	}
	keyRaw := make([]byte, 16)
	if _, err := rand.Read(keyRaw); err != nil {
		conn.Close()
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyRaw)
	path := u.RequestURI()
	if path == "" {
		path = "/"
	}
	req := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + u.Host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"User-Agent: codefree2api\r\n\r\n"
	if err := conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		conn.Close()
		return nil, err
	}
	if _, err := io.WriteString(conn, req); err != nil {
		conn.Close()
		return nil, err
	}
	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, err
	}
	if !strings.Contains(status, "101") {
		conn.Close()
		return nil, fmt.Errorf("websocket")
	}
	accept := ""
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(name), "Sec-WebSocket-Accept") {
			accept = strings.TrimSpace(value)
		}
	}
	if accept != acceptKey(key) {
		conn.Close()
		return nil, fmt.Errorf("websocket")
	}
	_ = conn.SetDeadline(time.Time{})
	c := &Conn{conn: conn, r: br, done: make(chan struct{})}
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-c.done:
		}
	}()
	return c, nil
}

func acceptKey(key string) string {
	sum := sha1.Sum([]byte(key + wsGUID))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func (c *Conn) Close() error {
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	return c.conn.Close()
}

// WriteText sends one masked text frame wrapped as a channel message.
func (c *Conn) WriteChannel(payload []byte) error {
	return c.WriteText([]byte(channelStart + string(payload) + channelEnd))
}

// WriteText sends one masked text frame.
func (c *Conn) WriteText(payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return writeFrame(c.conn, opText, payload, true)
}

// ReadChannel reads text frames until a channel message is complete.
func (c *Conn) ReadChannel() ([]byte, error) {
	buf := c.pending
	for {
		msg, rest, ok := takeChannel(buf)
		if ok {
			c.pending = rest
			return []byte(msg), nil
		}
		text, err := c.ReadText()
		if err != nil {
			c.pending = rest
			return nil, err
		}
		buf = rest + text
	}
}

func takeChannel(buf string) (msg, rest string, ok bool) {
	i := strings.Index(buf, channelStart)
	if i < 0 {
		if len(buf) > len(channelStart) {
			return "", buf[len(buf)-len(channelStart):], false
		}
		return "", buf, false
	}
	buf = buf[i+len(channelStart):]
	j := strings.Index(buf, channelEnd)
	if j < 0 {
		return "", channelStart + buf, false
	}
	return buf[:j], buf[j+len(channelEnd):], true
}

// ReadText reads one text message, answering ping frames.
func (c *Conn) ReadText() (string, error) {
	var acc []byte
	for {
		fin, op, payload, err := readFrame(c.r)
		if err != nil {
			return "", err
		}
		switch op {
		case opText, 0x0:
			acc = append(acc, payload...)
			if fin {
				return string(acc), nil
			}
		case opPing:
			c.mu.Lock()
			err = writeFrame(c.conn, opPong, payload, true)
			c.mu.Unlock()
			if err != nil {
				return "", err
			}
		case opClose:
			return "", io.EOF
		default:
		}
	}
}

func writeFrame(w io.Writer, op byte, payload []byte, mask bool) error {
	if len(payload) > maxFrame {
		return fmt.Errorf("frame")
	}
	header := []byte{0x80 | op, 0}
	if mask {
		header[1] |= 0x80
	}
	n := len(payload)
	switch {
	case n < 126:
		header[1] |= byte(n)
	case n <= 0xffff:
		header[1] |= 126
		header = append(header, byte(n>>8), byte(n))
	default:
		header[1] |= 127
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		header = append(header, ext[:]...)
	}
	var key [4]byte
	body := payload
	if mask {
		if _, err := rand.Read(key[:]); err != nil {
			return err
		}
		header = append(header, key[:]...)
		body = append([]byte(nil), payload...)
		for i := range body {
			body[i] ^= key[i%4]
		}
	}
	if _, err := w.Write(header); err != nil {
		return err
	}
	_, err := w.Write(body)
	return err
}

func readFrame(r *bufio.Reader) (fin bool, op byte, payload []byte, err error) {
	b0, err := r.ReadByte()
	if err != nil {
		return false, 0, nil, err
	}
	b1, err := r.ReadByte()
	if err != nil {
		return false, 0, nil, err
	}
	fin = b0&0x80 != 0
	op = b0 & 0x0f
	masked := b1&0x80 != 0
	ln := uint64(b1 & 0x7f)
	switch ln {
	case 126:
		var ext [2]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return false, 0, nil, err
		}
		ln = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err = io.ReadFull(r, ext[:]); err != nil {
			return false, 0, nil, err
		}
		ln = binary.BigEndian.Uint64(ext[:])
	}
	if ln > maxFrame {
		return false, 0, nil, fmt.Errorf("frame")
	}
	var key [4]byte
	if masked {
		if _, err = io.ReadFull(r, key[:]); err != nil {
			return false, 0, nil, err
		}
	}
	payload = make([]byte, ln)
	if _, err = io.ReadFull(r, payload); err != nil {
		return false, 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= key[i%4]
		}
	}
	return fin, op, payload, nil
}
