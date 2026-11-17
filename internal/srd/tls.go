package srd

import (
	"context"
	"crypto/tls"
	"net"
)

func tlsClient(ctx context.Context, raw net.Conn, serverName string) (net.Conn, error) {
	conn := tls.Client(raw, &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12})
	if err := conn.HandshakeContext(ctx); err != nil {
		return nil, err
	}
	return conn, nil
}
