package srd

import (
	"strconv"
	"strings"
	"time"
)

const expirySkew = 30 * time.Second

// usable reports whether the cf token can be sent. A zero expiry means the
// login response did not say when it ends.
func (s *Session) usable(now time.Time) bool {
	if s == nil || strings.TrimSpace(s.UserID) == "" || strings.TrimSpace(s.CFToken) == "" {
		return false
	}
	if s.CFExpiresAt <= 0 {
		return true
	}
	return now.Add(expirySkew).UnixMilli() < s.CFExpiresAt
}

// refreshable reports whether this process may exchange its own refresh token.
func (s *Session) refreshable(now time.Time) bool {
	if s == nil || strings.TrimSpace(s.RefreshToken) == "" || strings.TrimSpace(s.UserID) == "" {
		return false
	}
	if s.RefreshExpiresAt <= 0 {
		return true
	}
	return now.UnixMilli() < s.RefreshExpiresAt
}

func takeRefresh(s *Session, body []byte, now time.Time) {
	if s.RefreshToken == "" {
		s.RefreshToken = firstField(body, "x_cf_refresh_token")
	}
	if s.RefreshToken == "" {
		s.RefreshToken = firstField(body, "refresh_token")
	}
	if s.RefreshExpiresAt != 0 {
		return
	}
	if v := firstField(body, "refreshTokenExpiresAt"); v != "" {
		s.RefreshExpiresAt = asAbsolute(v)
		return
	}
	if v := firstField(body, "x_cf_refresh_token_expires_in"); v != "" {
		s.RefreshExpiresAt = asDuration(now, v)
		return
	}
	if v := firstField(body, "refresh_token_expires_in"); v != "" {
		s.RefreshExpiresAt = asDuration(now, v)
	}
}

func takeCFExpiry(s *Session, body []byte, now time.Time) {
	if s.CFExpiresAt != 0 {
		return
	}
	if v := firstField(body, "cfTokenExpiresAt"); v != "" {
		s.CFExpiresAt = asAbsolute(v)
		return
	}
	if v := firstField(body, "x_cf_token_expires_in"); v != "" {
		s.CFExpiresAt = asDuration(now, v)
		return
	}
	if v := firstField(body, "expires_in"); v != "" {
		s.CFExpiresAt = asDuration(now, v)
	}
}

func asAbsolute(raw string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	if n < 1_000_000_000_000 {
		return n * 1000
	}
	return n
}

func asDuration(now time.Time, raw string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return now.Add(time.Duration(n) * time.Second).UnixMilli()
}
