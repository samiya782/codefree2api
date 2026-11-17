package srd

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"time"
)

// AcquireOptions finds a session without printing it.
type AcquireOptions struct {
	App     App
	HTTP    *http.Client
	Now     func() time.Time
	Open    func(string) error
	Stderr  io.Writer
	Session *Session
	// Force skips saved and desktop logins and opens the page.
	Force bool
}

// Acquire uses a saved login, then the installed client's login, then the page.
// fromOwn is true when the session belongs to this program's saved file.
// The installed client's refresh token is never exchanged.
func Acquire(ctx context.Context, opt AcquireOptions) (sess *Session, fromOwn bool, err error) {
	now := time.Now()
	if opt.Now != nil {
		now = opt.Now()
	}
	if !opt.Force && opt.Session != nil && opt.Session.usable(now) {
		return opt.Session, false, nil
	}
	if !opt.Force {
		if own, _ := LoadOwn(opt.App.Kind); own != nil {
			if own.usable(now) {
				return own, true, nil
			}
			if own.refreshable(now) {
				next, rerr := Refresh(ctx, opt, own)
				if rerr == nil && next.usable(now) {
					_ = SaveOwn(opt.App.Kind, next)
					return next, true, nil
				}
			}
		}
		if desk := LoadDesktop(opt.App.Kind, now); desk != nil {
			return desk, false, nil
		}
	}
	sess, err = Login(ctx, LoginOptions{
		App:    opt.App,
		HTTP:   opt.HTTP,
		Now:    opt.Now,
		Open:   opt.Open,
		Stderr: opt.Stderr,
	})
	if err != nil {
		return nil, false, err
	}
	_ = SaveOwn(opt.App.Kind, sess)
	return sess, true, nil
}

// Refresh exchanges this program's refresh token. It follows the desktop
// client's refresh grant, including the shipped client secret. Callers must
// not pass the desktop program's refresh token.
func Refresh(ctx context.Context, opt AcquireOptions, prev *Session) (*Session, error) {
	if prev == nil || prev.RefreshToken == "" {
		return nil, ErrLogin
	}
	form := url.Values{}
	form.Set("client_id", opt.App.ClientID)
	form.Set("client_secret", opt.App.ClientSecret)
	form.Set("grant_type", "refresh_x_cf_token")
	form.Set("x_cf_refresh_token", prev.RefreshToken)
	header := make(http.Header)
	header.Set("Content-Type", "application/x-www-form-urlencoded")
	caller := &Caller{App: opt.App, HTTP: opt.HTTP, Now: opt.Now}
	status, body, err := caller.Do(ctx, Call{
		Method:   http.MethodPost,
		Path:     tokenPath,
		Body:     []byte(form.Encode()),
		Header:   header,
		Unsigned: true,
	})
	if err != nil || status != http.StatusOK {
		return nil, ErrLogin
	}
	now := time.Now()
	if opt.Now != nil {
		now = opt.Now()
	}
	cf := firstField(body, "x_cf_token")
	if cf == "" {
		cf = firstField(body, "access_token")
	}
	if cf == "" {
		return nil, ErrLogin
	}
	next := *prev
	next.CFToken = cf
	next.CFExpiresAt = 0
	next.RefreshExpiresAt = 0
	next.RefreshToken = ""
	takeRefresh(&next, body, now)
	takeCFExpiry(&next, body, now)
	if next.RefreshToken == "" {
		next.RefreshToken = prev.RefreshToken
		next.RefreshExpiresAt = prev.RefreshExpiresAt
	}
	if id := firstField(body, "uid"); id != "" {
		next.UserID = id
	}
	if sid := firstField(body, "ori_session_id"); sid != "" {
		next.SessionID = sid
	} else if sid := firstField(body, "session_id"); sid != "" {
		next.SessionID = sid
	}
	if next.UserID == "" || next.CFToken == "" {
		return nil, ErrLogin
	}
	return &next, nil
}
