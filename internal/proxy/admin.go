package proxy

import (
	"context"
	"html/template"
	"log"
	"net/http"
	"time"
)

var adminTmpl = template.Must(template.New("admin").Parse(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>codefree2api</title>
<style>
  :root { color-scheme: light dark; }
  body { font-family: ui-sans-serif, system-ui, sans-serif; margin: 2rem auto; max-width: 52rem; line-height: 1.5; }
  h1 { font-size: 1.4rem; }
  dl { display: grid; grid-template-columns: 8rem 1fr; gap: 0.35rem 1rem; }
  dt { font-weight: 600; }
  ul { padding-left: 1.2rem; }
  .ok { color: #0a0; }
  .err { color: #c00; }
  .muted { opacity: 0.7; }
  code { font-family: ui-monospace, monospace; }
</style>
</head>
<body>
  <h1>codefree2api</h1>
  <p class="muted">本机 HTTP 代理，上游为已登录的 <code>codefree-o</code>。仅监听 127.0.0.1。</p>
  <h2>运行状态</h2>
  <dl>
    <dt>监听地址</dt><dd><code>{{.Addr}}</code></dd>
    <dt>二进制</dt><dd><code>{{.Bin}}</code></dd>
  </dl>
  <h2>模型列表</h2>
  {{if .ModelsErr}}<p class="err">{{.ModelsErr}}</p>{{end}}
  {{if .Models}}
  <ul>{{range .Models}}<li><code>{{.}}</code></li>{{end}}</ul>
  {{else}}
  <p class="muted">暂无模型。上游 provider 列表里没有 codefree 模型。</p>
  {{end}}
  <h2>累计</h2>
  <dl>
    <dt>请求</dt><dd>{{.Stats.Requests}}（成功 {{.Stats.OK}}，失败 {{.Stats.Errors}}）</dd>
    <dt>输入 token</dt><dd>{{.Stats.InputTokens}}</dd>
    <dt>输出 token</dt><dd>{{.Stats.OutputTokens}}</dd>
  </dl>
  <h2>最近一次调用</h2>
  {{if .HasLast}}
  <dl>
    <dt>时间</dt><dd>{{.LastAt}}</dd>
    <dt>端点</dt><dd><code>{{.Last.Endpoint}}</code></dd>
    <dt>模型</dt><dd><code>{{.Last.Model}}</code></dd>
    <dt>耗时</dt><dd>{{.LastDur}}</dd>
    <dt>token</dt><dd>in {{.Last.InputTokens}} / out {{.Last.OutputTokens}}</dd>
    <dt>状态</dt>
    <dd>{{if .Last.OK}}<span class="ok">成功</span>{{else}}<span class="err">失败</span>{{end}}</dd>
    {{if .Last.Error}}<dt>错误</dt><dd class="err">{{.Last.Error}}</dd>{{end}}
  </dl>
  {{else}}
  <p class="muted">尚未调用。</p>
  {{end}}
</body>
</html>`))

type adminData struct {
	Addr      string
	Bin       string
	Models    []string
	ModelsErr string
	HasLast   bool
	Last      LastCall
	LastAt    string
	LastDur   string
	Stats     Stats
}

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	last, cached, stats := s.snapshot()
	models := cached
	modelsErr := ""
	ctx, cancel := context.WithTimeout(r.Context(), listTimeout)
	defer cancel()
	if live, err := s.engine.Models(ctx); err != nil {
		modelsErr = err.Error()
	} else {
		models = make([]string, 0, len(live))
		for _, m := range live {
			if m.ID != "" {
				models = append(models, m.ID)
			}
		}
		s.mu.Lock()
		s.modelCache = append([]string(nil), models...)
		s.mu.Unlock()
	}

	data := adminData{
		Addr:      "http://" + s.addr,
		Bin:       s.bin,
		Models:    models,
		ModelsErr: modelsErr,
		HasLast:   !last.At.IsZero(),
		Last:      last,
		Stats:     stats,
	}
	if data.HasLast {
		data.LastAt = last.At.Format(time.RFC3339)
		data.LastDur = last.Duration.String()
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := adminTmpl.Execute(w, data); err != nil {
		log.Printf("admin template: %v", err)
	}
}
