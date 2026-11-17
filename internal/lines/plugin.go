package lines

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"codefree2api/internal/srd"
)

func runPlugin(ctx context.Context, parsed lineArgs, opt Options) error {
	if parsed.accepted == 0 {
		return writeCounts(opt.Stdout, parsed.accepted, parsed.effective)
	}
	dir, err := opt.dir()
	if err != nil {
		return err
	}
	urls, gitURL, err := loadRemotes(ctx, dir, parsed)
	if err != nil {
		return err
	}
	editor, err := editorVersion(ctx, opt, parsed.editorVersion)
	if err != nil {
		return err
	}
	app := clientApp(srd.KindPlugin, opt.BaseURL)
	sess, err := ensureSession(ctx, opt, app)
	if err != nil {
		return err
	}
	caller := callerFor(opt, app, sess)
	pluginVersion := parsed.pluginVersion
	if pluginVersion == "" {
		pluginVersion, err = lookupPluginVersion(ctx, caller)
		if err != nil {
			return err
		}
	}
	body := pluginBody{
		UserID:    mustUserID(sess.UserID),
		ProjectID: 0,
		App:       "aicode",
		Service:   "agent",
		Time:      formatPluginTime(opt.Now()),
		Client: pluginClient{
			Type:          "vscode",
			Version:       editor,
			PluginVersion: pluginVersion,
			GitURL:        gitURL,
			GitURLs:       urls,
			ProjectName:   "",
		},
		Activities: []pluginActivity{
			{Key: "agent_accepted_code_line", SubKey: "agent", Value: activityValue{Count: parsed.accepted}},
			{Key: "agent_accepted_code_count", SubKey: "agent", Value: activityValue{Count: 1}},
		},
	}
	if body.UserID == 0 && sess.UserID != "0" {
		return srd.ErrLogin
	}
	payload, err := srd.Marshal(body)
	if err != nil {
		return errReport
	}
	status, raw, err := caller.Do(ctx, srd.Call{
		Method:    http.MethodPost,
		Path:      recordPath,
		Body:      payload,
		ProjectID: "0",
		GitURLs:   urls,
	})
	if err != nil || !pluginOK(status, raw) {
		return errReport
	}
	return writeCounts(opt.Stdout, parsed.accepted, parsed.effective)
}

type pluginBody struct {
	UserID     int64            `json:"userId"`
	ProjectID  int64            `json:"projectId"`
	App        string           `json:"app"`
	Service    string           `json:"service"`
	Time       string           `json:"time"`
	Client     pluginClient     `json:"client"`
	Activities []pluginActivity `json:"activities"`
}

type pluginClient struct {
	Type          string   `json:"type"`
	Version       string   `json:"version"`
	PluginVersion string   `json:"pluginVersion"`
	GitURL        string   `json:"gitUrl"`
	GitURLs       []string `json:"gitUrls"`
	ProjectName   string   `json:"projectName"`
}

type pluginActivity struct {
	Key    string        `json:"key"`
	SubKey string        `json:"subKey"`
	Value  activityValue `json:"value"`
}

func formatPluginTime(t time.Time) string {
	t = t.In(time.Local)
	return fmt.Sprintf("%04d-%d-%d %02d:%d:%d.%03d", t.Year(), int(t.Month()), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond()/1e6)
}

func lookupPluginVersion(ctx context.Context, caller *srd.Caller) (string, error) {
	status, body, err := caller.Do(ctx, srd.Call{
		Method:    http.MethodGet,
		Path:      "/api/composqback/component/v1/detail",
		Query:     url.Values{"componentId": {"39999"}},
		ProjectID: "0",
	})
	if err != nil || status != http.StatusOK {
		return "", errNoPluginVersion
	}
	found := srd.LatestVersion(body)
	if found == "" {
		return "", errNoPluginVersion
	}
	return found, nil
}

func pluginOK(status int, body []byte) bool {
	if status != http.StatusOK {
		return false
	}
	n, ok := jsonInt(body, "optResult")
	return ok && n == 0
}

func jsonInt(body []byte, key string) (int, bool) {
	dec := json.NewDecoder(bytesReader(body))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return 0, false
	}
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case json.Number:
		n, err := t.Int64()
		if err != nil {
			return 0, false
		}
		return int(n), true
	default:
		return 0, false
	}
}

func mustUserID(raw string) int64 {
	n, err := parseCount(raw)
	if err != nil {
		return 0
	}
	return n
}
