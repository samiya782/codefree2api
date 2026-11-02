# codefree2api

把本机已登录的 [`codefree-o`](https://www.npmjs.com/package/@srdcloud/codefree-o) 暴露成 OpenAI / Anthropic 兼容的本地 HTTP 代理，供 Cursor、Claude Code、Codex、普通 `curl` 使用。

代理懒启动一个 `codefree-o serve`（只绑 `127.0.0.1`，随机 HTTP Basic 口令，不写进日志）。工作目录是系统临时目录下的 `codefree2api-work`，避免代理里的默认助手改到你当前的仓库。子进程沿用当前环境里的登录态。

## 与 opencode2api 的差异

上游不同，所以不是 opencode2api 的逐项移植。opencode2api 把请求转到 OpenCode Zen / Go 的 HTTP API；本项目把请求转到本机已登录的 `codefree-o serve`。

| | opencode2api | codefree2api |
|---|---|---|
| 上游 | OpenCode Zen / OpenCode Go 的 HTTP API | 本机 `codefree-o serve` |
| 模型列表 | Zen/Go 的 models 接口 | `GET /provider` 里的 `codefree/` 模型；失败时回退 `codefree-o models --pure` |
| 推理 | 远程会话 API | 同一次 serve 进程里的 session。普通对话会流式返回；带客户端工具时走同步 `/message` |
| 鉴权凭据 | `public` / `zen:` / `go:` 上游 key | **不传 `api_key`，不设置 `SRDCLOUD_*`**，沿用本机已有登录态 |
| 监听 | 可按部署暴露 | **只绑定 `127.0.0.1`** |

本项目标准库实现，无第三方依赖。Go 1.22+。

### 已接上的客户端能力

- `GET /health`（免鉴权）
- `GET /v1/models`
- `POST /v1/chat/completions`，含 SSE：role、`reasoning_content`、content、`tool_calls`、`finish_reason`、可选 `stream_options.include_usage`、`data: [DONE]`
- `POST /v1/messages`，含 `content_block_start`、`content_block_delta`、`content_block_stop`、`message_delta`、`message_stop`，以及 `tool_use`
- `POST /v1/messages/count_tokens`
- `POST /v1/responses`（Codex wire API），含 `previous_response_id` 续写
- `Authorization: Bearer` 与 `x-api-key`。Bearer 写错时不会再去认 `x-api-key`
- 工具：`tools`、`tool_choice`、`tool_use` / `tool_result`、OpenAI `tool_calls`。带工具时会话权限为拒绝助手自己执行命令，结果按 JSON 工具信封解析
- 图片和文件，转成 `codefree-o` 的 file part（`data:` URL 会落成临时文件）
- `max_tokens`（系统提示里的长度提示，再按本地估算截断）、`stop`（本地截断）
- `reasoning_effort`、`reasoning.effort`、`output_config.effort`、Anthropic `thinking` 会映射成 variant。若上游因 variant 返回 400 且尚未输出，会去掉 variant 再试一次
- token usage。优先用上游返回的 input / output / reasoning / cache；上游为 0 时用本地估算补上
- `model_aliases`：客户端模型名映射到 `codefree/<id>`，响应里仍回显客户端传来的名字
- `codefree2api launch claude` / `codefree2api launch codex`：先起代理，再 exec 对应 CLI
- `GET /admin` 状态页，`GET /api/stats` 用量（请求数、成功、失败、输入/输出 token）

### 只在本地处理、不会传给上游的字段

- `temperature`：Chat / Responses 接受 0 到 2，Messages 接受 0 到 1。超出范围返回 400。合法值不改变上游采样。
- `top_p`：接受，不改变上游采样。
- `POST /v1/messages/count_tokens`：按文本做本地估算（`(rune 数 + 1) / 2`），不是模型分词器。
- `launch` 只设置 CLI 的 base URL 和 key，然后 exec。没有 TUI，也不替你登录。
- 部分模型的附件能力标记是关闭的。图片仍会作为 file part 送出；上游若拒绝，错误会原样回到客户端。

### 不照搬的上游能力

这些绑在 OpenCode Zen/Go 的 HTTP 网关上，和本机 `codefree-o` 对不上，故意不做：

- `public` / `zen:` / `go:` 多级上游 key、key 池
- SOCKS5、轮询出口、粘性代理
- `protocol_rules`、转发到 `/zen/v1/*`
- models.dev 目录、免费层 `-free` 映射、请求指纹
- prompt cache、`cache_control`
- `POST /v1/systemone`
- OpenCode Console 的 `/inference/*` 路径
- Docker / GHCR、多平台发布包、一键安装脚本
- 管理页上的配置编辑器和登录页

## 准备

1. 本机已安装并可运行 `codefree-o`（默认路径见下方）。
2. 已用 `codefree-o` 完成登录。本代理不会读取或导出 `~/.codefree-o` 下的凭据文件。
3. Go 1.22+。

默认二进制：

```
/home/samiya/.local/share/mise/installs/node/24.22.0/bin/codefree-o
```

可用 `-bin` 或配置文件覆盖。

## 启动

```bash
go build -o codefree2api ./cmd/codefree2api
./codefree2api
```

常用参数：

```bash
./codefree2api -port 8000 -config config.json
./codefree2api -bin /home/samiya/.local/share/mise/installs/node/24.22.0/bin/codefree-o
./codefree2api -password '本地口令'
```

| 参数 | 默认 | 说明 |
|---|---|---|
| `-port` | `8000` | 仅监听 `127.0.0.1:<port>` |
| `-config` | `config.json` | 可选 JSON；文件不存在则忽略 |
| `-password` | 空 | 设置后除 `/health` 外要求 Bearer 或 `x-api-key` |
| `-bin` | 见上 | 覆盖 `codefree-o` 路径 |

命令行显式传入的参数优先于配置文件。配置示例见 `config.example.json`：

```json
{
  "port": 8000,
  "password": "",
  "bin": "/home/samiya/.local/share/mise/installs/node/24.22.0/bin/codefree-o",
  "model_aliases": {
    "sonnet": "codefree/GLM-5.2"
  }
}
```

`model_aliases` 的值应是 `codefree/<模型 id>`。未设置 password 时本机免鉴权。

直接拉起客户端：

```bash
./codefree2api launch claude --model codefree/GLM-5.2
./codefree2api launch codex --model codefree/GLM-5.2
./codefree2api launch claude -- --print
```

`claude` 会带上 `ANTHROPIC_BASE_URL=http://127.0.0.1:<port>` 和 `ANTHROPIC_API_KEY`（口令为空时用 `local`）。`codex` 会把 provider 指到 `http://127.0.0.1:<port>/v1`，wire API 为 `responses`，key 放在 `CODEFREE2API_OPENAI_API_KEY`。`--` 后面的参数原样传给 CLI。

## 接口

- `GET /health` → `{"status":"ok"}`（始终免鉴权）
- `GET /v1/models` → OpenAI models 列表
- `POST /v1/chat/completions` → OpenAI Chat Completions（`stream=false|true`）
- `POST /v1/messages` → Anthropic Messages
- `POST /v1/messages/count_tokens` → `{"input_tokens": <估算>}`
- `POST /v1/responses` → OpenAI Responses。`previous_response_id` 在本进程内续写同一个上游 session，最多记住 256 条；未知 id 返回 400
- `GET /admin` → 状态页（监听地址、二进制、模型、累计 token、最近一次调用）
- `GET /api/stats` → 同一套用量的 JSON

设置口令后，除 `/health` 外都要 `Authorization: Bearer <password>` 或 `x-api-key: <password>`。`/v1/messages` 的鉴权失败使用 Anthropic 错误形状。没有登录绕过。

普通对话每次会把完整历史放进新 session。只有 Responses 的 `previous_response_id` 会接着上次的上游 session，并且只发送这次的新输入。

上游单次调用超时 10 分钟。`codefree-o` 由 `exec.Command` 启动，不走 shell。代理收到 SIGINT 或 SIGTERM 时，以及 `launch` 的客户端退出时，会关掉这个 serve 子进程。

## curl 示例

健康检查：

```bash
curl -s http://127.0.0.1:8000/health
```

模型列表：

```bash
curl -s http://127.0.0.1:8000/v1/models
```

非流式对话：

```bash
curl -s http://127.0.0.1:8000/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"codefree/GLM-5.2","messages":[{"role":"user","content":"ping"}]}'
```

流式（SSE，最后一帧为 `data: [DONE]`）：

```bash
curl -N http://127.0.0.1:8000/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"codefree/GLM-5.2","stream":true,"messages":[{"role":"user","content":"ping"}]}'
```

Anthropic Messages（也可用 `x-api-key`）：

```bash
curl -s http://127.0.0.1:8000/v1/messages \
  -H 'Content-Type: application/json' \
  -H 'x-api-key: 你的口令' \
  -d '{"model":"codefree/GLM-5.2","max_tokens":256,"messages":[{"role":"user","content":"ping"}]}'
```

Codex / Responses：

```bash
curl -s http://127.0.0.1:8000/v1/responses \
  -H 'Content-Type: application/json' \
  -d '{"model":"codefree/GLM-5.2","input":"ping"}'
```

开启口令后：

```bash
curl -s http://127.0.0.1:8000/v1/models \
  -H 'Authorization: Bearer 你的口令'
```

OpenAI 兼容客户端把 `base_url` 设为 `http://127.0.0.1:8000/v1`，`api_key` 填口令（未设置口令时可填任意占位）。Anthropic 兼容客户端把 endpoint 指到 `http://127.0.0.1:8000`。Claude Code 使用 `ANTHROPIC_BASE_URL` 和 `ANTHROPIC_API_KEY`。Codex 使用 Responses base URL。

## 安全边界

- **只绑定 `127.0.0.1`**，不接受外部网卡连接。不要把端口映射到公网。
- **不读取、不打印、不导出** `~/.codefree-o` 下的凭据文件；子进程沿用当前用户已有登录态。
- **不向 `codefree-o` 传递 `api_key`，不设置 `SRDCLOUD_*`。**
- 给 `codefree-o serve` 的 Basic 口令是进程内随机值，只用于本机回环，不会写进响应。
- 可选 Bearer / `x-api-key` 只保护本代理的 HTTP 接口，不是上游账号密码。未设置时本机进程即可调用，适合单人笔记本，不适合共享主机。
- `/admin`、`/api/stats` 与业务接口使用同一套口令，没有后门。
- 不要把 `config.json` 里的口令提交到 git。

## 开发

```bash
go test ./...
CODEFREE2API_LIVE=1 go test ./internal/proxy -run TestLivePing
go build -o codefree2api ./cmd/codefree2api
```

`TestLivePing` 会用模型列表里的第一个模型发一句 “Reply with the single word pong.”。未设置环境变量时跳过。登录不可用时也跳过。
