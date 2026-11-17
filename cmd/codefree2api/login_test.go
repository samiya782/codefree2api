package main

import (
	"context"
	"strings"
	"testing"
)

func TestLoginUsage(t *testing.T) {
	err := runLogin(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "login --client") {
		t.Fatal("usage")
	}
	err = runLogin(context.Background(), []string{"--client", "codefree-o"})
	if err == nil || !strings.Contains(err.Error(), "login --client") {
		t.Fatal("client")
	}
}
