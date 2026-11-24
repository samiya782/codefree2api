package lines

import (
	"context"
	"net/http"
	"net/url"

	"codefree2api/internal/srd"
)

const defaultSpaceVersion = "1.5.1"

func runSpace(ctx context.Context, parsed lineArgs, opt Options) error {
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
	app := clientApp(srd.KindSpace, opt.BaseURL)
	sess, err := ensureSession(ctx, opt, app)
	if err != nil {
		return err
	}
	caller := callerFor(opt, app, sess)
	version := parsed.spaceVersion
	if version == "" {
		version = lookupSpaceVersion(ctx, caller)
	}
	projectID, projectName := lookupProject(ctx, caller, urls, version)
	userID := mustUserID(sess.UserID)
	if userID == 0 && sess.UserID != "0" {
		return srd.ErrLogin
	}
	stamp := formatTime(opt.Now())
	appName := "codefree-ide"
	service := "ide_agent"
	subKey := "Craft"
	prefix := "ide_agent_"
	if parsed.spaceSpec {
		appName = "codefree-ide-spec"
		service = "ide-digital-employee"
		subKey = "Unknown"
		prefix = "agent_"
	}
	reqID, err := newNonce()
	if err != nil {
		return errReport
	}
	records := []struct {
		key   string
		count int64
		req   bool
	}{
		{key: prefix + "gen_code_line", count: parsed.accepted, req: true},
		{key: prefix + "gen_code_count", count: 1},
		{key: prefix + "accepted_code_line", count: parsed.accepted, req: true},
		{key: prefix + "accepted_code_count", count: 1},
	}
	for _, item := range records {
		body := map[string]any{
			"userid":    userID,
			"projectId": projectID,
			"app":       appName,
			"service":   service,
			"time":      stamp,
			"client": map[string]any{
				"type":        "ide-win32",
				"version":     version,
				"gitUrl":      gitURL,
				"gitUrls":     urls,
				"projectName": projectName,
			},
			"activities": []any{map[string]any{
				"key":    item.key,
				"subKey": subKey,
				"value":  map[string]any{"count": item.count},
			}},
		}
		if item.req {
			body["reqId"] = reqID
		}
		payload, err := srd.Marshal(body)
		if err != nil {
			return errReport
		}
		header := make(http.Header)
		header.Set("Content-Type", "text/plain;charset=UTF-8")
		status, raw, err := caller.Do(ctx, srd.Call{
			Method:    http.MethodPost,
			Path:      recordPath,
			Body:      payload,
			Header:    header,
			ProjectID: fmtProject(projectID),
			GitURLs:   urls,
		})
		if err != nil || !spaceOK(status, raw) {
			return errReport
		}
	}
	return writeCounts(opt.Stdout, parsed.accepted, parsed.effective)
}

func lookupSpaceVersion(ctx context.Context, caller *srd.Caller) string {
	status, body, err := caller.Do(ctx, srd.Call{
		Method:    http.MethodGet,
		Path:      "/api/acbackend/ideservice/v1/ide-releases",
		Query:     url.Values{"onlyLast": {"true"}},
		ProjectID: "0",
	})
	if err != nil || status != http.StatusOK {
		return defaultSpaceVersion
	}
	if found := srd.SpaceVersion(body); found != "" {
		return found
	}
	return defaultSpaceVersion
}

func lookupProject(ctx context.Context, caller *srd.Caller, urls []string, version string) (int64, string) {
	payload, err := srd.Marshal(map[string]any{
		"gitUrls":    urls,
		"clientType": "codefree-ide",
		"version":    version,
	})
	if err != nil {
		return 0, ""
	}
	status, body, err := caller.Do(ctx, srd.Call{
		Method:    http.MethodPost,
		Path:      "/api/acbackend/usermanager/v1/users/project",
		Body:      payload,
		ProjectID: "0",
		GitURLs:   urls,
	})
	if err != nil || status != http.StatusOK {
		return 0, ""
	}
	return srd.Project(body)
}

func fmtProject(id int64) string {
	return fmtInt(id)
}

func spaceOK(status int, body []byte) bool {
	if status != http.StatusOK {
		return false
	}
	code, hasCode := jsonInt(body, "code")
	opt, hasOpt := jsonInt(body, "optResult")
	if !hasCode && !hasOpt {
		return false
	}
	return (hasCode && code == 0) || (hasOpt && opt == 0)
}
