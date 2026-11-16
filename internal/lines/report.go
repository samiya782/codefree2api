package lines

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

var activityKeys = []struct {
	key   string
	lines bool
}{
	{key: "tools_gen_code_count"},
	{key: "tools_gen_code_line", lines: true},
	{key: "tools_gen_accepted_code_line", lines: true},
	{key: "tools_gen_accepted_code_count"},
}

type recordBody struct {
	UserID     int64        `json:"userId"`
	App        string       `json:"app"`
	Service    string       `json:"service"`
	Time       string       `json:"time"`
	Client     recordClient `json:"client"`
	Activities []activity   `json:"activities"`
}

type recordClient struct {
	Type    string   `json:"type"`
	Version string   `json:"version"`
	GitURLs []string `json:"gitUrls"`
}

type activity struct {
	Key    string        `json:"key"`
	SubKey string        `json:"subKey"`
	Value  activityValue `json:"value"`
}

type activityValue struct {
	Count int64 `json:"count"`
}

func (l *login) report(ctx context.Context, accepted int64, gitURLs []string) error {
	userID, err := parseCount(l.userID)
	if err != nil {
		return errNoLogin
	}
	stamp := formatTime(l.now())
	for _, item := range activityKeys {
		count := int64(1)
		if item.lines {
			count = accepted
		}
		body := recordBody{
			UserID:  userID,
			App:     "codefree-o",
			Service: "codefree-o",
			Time:    stamp,
			Client: recordClient{
				Type:    "codefree-o",
				Version: l.version,
				GitURLs: gitURLs,
			},
			Activities: []activity{{
				Key:    item.key,
				SubKey: "edit",
				Value:  activityValue{Count: count},
			}},
		}
		if err := l.postActivity(ctx, body, true); err != nil {
			return err
		}
	}
	return nil
}

func (l *login) postActivity(ctx context.Context, body recordBody, allowRefresh bool) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return errReport
	}
	status, opt, err := l.post(ctx, payload)
	if err != nil {
		return errReport
	}
	if status == http.StatusUnauthorized && l.mode == "oauth" && allowRefresh {
		_, rec, err := readAuthFile(l.env)
		if err != nil {
			return errRefresh
		}
		if err := l.refresh(ctx, rec.refresh); err != nil {
			return err
		}
		return l.postActivity(ctx, body, false)
	}
	if status != http.StatusOK || opt == nil || *opt != 0 {
		return errReport
	}
	return nil
}

func (l *login) post(ctx context.Context, payload []byte) (int, *int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.baseURL+recordPath, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "opencode/"+l.version)
	req.Header.Set("userId", l.userID)
	req.Header.Set("projectId", l.projectID)
	if l.mode == "oauth" {
		nonce, err := newNonce()
		if err != nil {
			return 0, nil, err
		}
		timestamp := strconv.FormatInt(l.now().Unix(), 10)
		signature, err := sign(http.MethodPost, l.appID, timestamp, nonce)
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("X-Cf-Token", l.cfToken)
		req.Header.Set("X-Cf-AppId", l.appID)
		req.Header.Set("X-Cf-Timestamp", timestamp)
		req.Header.Set("X-Cf-Nonce", nonce)
		req.Header.Set("X-Cf-Signature", signature)
	} else {
		req.Header.Set("accessToken", l.accessToken())
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	var parsed struct {
		OptResult *int `json:"optResult"`
	}
	_ = json.Unmarshal(raw, &parsed)
	return resp.StatusCode, parsed.OptResult, nil
}

func (l *login) accessToken() string {
	return envGet(l.env, "SRDCLOUD_CF_ACCESS_TOKEN")
}

func formatTime(t time.Time) string {
	t = t.In(time.Local)
	return t.Format("2006-01-02 15:04:05.") + fmt.Sprintf("%03d", t.Nanosecond()/1e6)
}

func newNonce() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
