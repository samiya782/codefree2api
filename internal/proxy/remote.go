package proxy

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"codefree2api/internal/srd"
)

var (
	errNoModels = errors.New("没有模型")
	errQuota    = errors.New("额度已用尽")
	errUpstream = errors.New("上游请求失败")
	errAuth     = errors.New("unauthorized")
)

const (
	modelsPath      = "/api/aebackend/modelmanager/v1/assistant/models"
	completionsPath = "/api/acbackend/codechat/v1/completions"
	projectPath     = "/api/acbackend/usermanager/v1/users/project"
	releasesPath    = "/api/acbackend/ideservice/v1/ide-releases"
	detailPath      = "/api/composqback/component/v1/detail"
	wsPath          = "/websocket/peerAppgw"
)

// remote holds the in-process login shared by plugin and Space.
type remote struct {
	App     srd.App
	HTTP    *http.Client
	Now     func() time.Time
	Session *srd.Session
	Login   func(context.Context) (*srd.Session, error)

	mu    sync.Mutex
	sess  *srd.Session
	own   bool
	force bool
}

func (r *remote) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *remote) http() *http.Client {
	if r.HTTP != nil {
		return r.HTTP
	}
	return http.DefaultClient
}

func (r *remote) session(ctx context.Context) (*srd.Session, error) {
	r.mu.Lock()
	force := r.force
	sess := r.sess
	if sess == nil && !force {
		sess = r.Session
	}
	login := r.Login
	app := r.App
	client := r.http()
	now := r.Now
	r.mu.Unlock()
	if !force && sess != nil && sess.UserID != "" && sess.CFToken != "" {
		r.mu.Lock()
		r.sess = sess
		r.mu.Unlock()
		return sess, nil
	}
	var next *srd.Session
	var fromOwn bool
	var err error
	if login != nil {
		next, err = login(ctx)
	} else {
		next, fromOwn, err = srd.Acquire(ctx, srd.AcquireOptions{
			App:    app,
			HTTP:   client,
			Now:    now,
			Stderr: os.Stderr,
			Force:  force,
		})
	}
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.sess = next
	r.Session = next
	r.own = fromOwn
	r.force = false
	r.mu.Unlock()
	return next, nil
}

func (r *remote) clear() {
	r.mu.Lock()
	own := r.own
	kind := r.App.Kind
	r.sess = nil
	r.Session = nil
	r.own = false
	r.force = true
	r.mu.Unlock()
	if own {
		_ = srd.DeleteOwn(kind)
	}
}

func (r *remote) caller(sess *srd.Session) *srd.Caller {
	return &srd.Caller{App: r.App, HTTP: r.http(), Now: r.Now, Session: sess}
}

func normalizeClient(name string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "codefree-o":
		return "codefree-o", nil
	case "plugin", "space":
		return strings.ToLower(strings.TrimSpace(name)), nil
	default:
		return "", errors.New("未知客户端")
	}
}
