# codefree2api

把本机已登录的 [`codefree-o`](https://www.npmjs.com/package/@srdcloud/codefree-o) 暴露成 OpenAI / Anthropic 兼容的本地 HTTP 代理，供 Cursor、Claude Code、普通 `curl` 等客户端使用。

## 与 opencode2api 的差异

上游不同，所以不是 opencode2api 的逐项移植。opencode2api 把请求转到 OpenCode Zen / Go 的 HTTP API；本项目把请求收成一段纯文本，交给本机已登录的 `codefree-o run --pure --model <model>`（stdin）。`codefree-o` 自己还有 `--format json`、`--variant`、`--thinking`、`-f`、`--session`，当前代理都没有接上。

| | opencode2api | codefree2api |
|---|---|---|
| 上游 | OpenCode Zen / OpenCode Go 的 HTTP API | 本机二进制 `codefree-o` |
| 模型列表 | Zen/Go 的 models 接口 | `codefree-o models --pure`，只收录 `codefree/` 前缀 |
| 推理 | 远程会话 API | 每次调用新起一次 `codefree-o run`，从 stdin 读提示 |
| 鉴权凭据 | `public` / `zen:` / `go:` 上游 key | **不传 `api_key`，不设置 `SRDCLOUD_*`**，沿用本机已有登录态 |
| 监听 | 可按部署暴露 | **只绑定 `127.0.0.1`** |

本项目标准库实现，无第三方依赖。

### 已实现的对应能力

- `GET /health`
- `GET /v1/models`（OpenAI models 列表形状）
- `POST /v1/chat/completions` 非流式，以及 SSE 流式（首包 role、增量 content、`finish_reason=stop`、`data: [DONE]`）
- `POST /v1/messages` 非流式 text block；流式只有 `message_start`、`content_block_delta`、`message_stop`
- `messages[].content` 为字符串，或 `type=text` 的内容数组；Anthropic 顶层 `system` 会接到提示前面
- 可选 `Authorization: Bearer <password>`；`/health` 免鉴权
- `GET /admin` 状态页：监听地址、二进制路径、模型列表、最近一次调用

### 客户端会用到、当前没有的能力

这些在 opencode2api 里是正式接口，本代理还没有：

- `POST /v1/responses`（Codex 的 wire API）
- `POST /v1/messages/count_tokens`
- Anthropic 流式的 `content_block_start`、`content_block_stop`、`message_delta`
- `x-api-key`（Claude Code 常用这个头，这里只认 Bearer）
- 工具调用：`tools`、`tool_choice`、`tool_use`、`tool_result` 会被丢掉，只留文本
- 图片、文件。`codefree-o run -f` 能挂文件，代理没有转发
- `temperature`、`top_p`、`max_tokens`、`stop`、`reasoning_effort`、thinking。`max_tokens` 会读入但不会传给上游
- token usage。响应里的 usage 固定为 0
- 模型别名、会话续写（`previous_response_id`、`codefree-o --session`）
- `launch claude` / `launch codex`
- 管理接口：改配置、用量统计、刷新模型、登录页

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
| `-password` | 空 | 设置后除 `/health` 外要求 `Authorization: Bearer <password>` |
| `-bin` | 见上 | 覆盖 `codefree-o` 路径 |

命令行参数优先于配置文件。配置示例见 `config.example.json`：

```json
{
  "port": 8000,
  "password": "",
  "bin": "/home/samiya/.local/share/mise/installs/node/24.22.0/bin/codefree-o"
}
```

未设置 password 时本机免鉴权。

## 接口

- `GET /health` → `{"status":"ok"}`（始终免鉴权）
- `GET /v1/models` → OpenAI models 列表
- `POST /v1/chat/completions` → OpenAI Chat Completions（`stream=false|true`）
- `POST /v1/messages` → Anthropic Messages 最小兼容（非流式 text block；流式 `message_start` / `content_block_delta` / `message_stop`）
- `GET /admin` → 简单状态页（监听地址、二进制路径、模型列表、最近一次调用）。设置口令后同样需要 Bearer，没有登录绕过。

`messages[].content` 支持字符串，也支持 `[{"type":"text","text":"..."}]` 数组。提示会保留 role，原样把 `model` 传给 `codefree-o`。上游调用超时 10 分钟，使用 `exec.CommandContext`，不走 shell。

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

content parts：

```bash
curl -s http://127.0.0.1:8000/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"codefree/GLM-5.2","messages":[{"role":"user","content":[{"type":"text","text":"ping"}]}]}'
```

流式（SSE，最后一帧为 `data: [DONE]`）：

```bash
curl -N http://127.0.0.1:8000/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"codefree/GLM-5.2","stream":true,"messages":[{"role":"user","content":"ping"}]}'
```

Anthropic Messages：

```bash
curl -s http://127.0.0.1:8000/v1/messages \
  -H 'Content-Type: application/json' \
  -d '{"model":"codefree/GLM-5.2","max_tokens":256,"messages":[{"role":"user","content":"ping"}]}'
```

开启口令后：

```bash
curl -s http://127.0.0.1:8000/v1/models \
  -H 'Authorization: Bearer 你的口令'
```

OpenAI 兼容客户端把 `base_url` 设为 `http://127.0.0.1:8000/v1`，`api_key` 填口令（未设置口令时可填任意占位）。Anthropic 兼容客户端把 endpoint 指到 `http://127.0.0.1:8000`。

## 安全边界

- **只绑定 `127.0.0.1`**，不接受外部网卡连接。不要把端口映射到公网。
- **不读取、不打印、不导出** `~/.codefree-o` 下的凭据文件；子进程沿用当前用户已有登录态。
- **不向 `codefree-o` 传递 `api_key`，不设置 `SRDCLOUD_*`。**
- 可选 Bearer 只保护本代理的 HTTP 接口，不是上游账号密码。未设置时本机进程即可调用，适合单人笔记本，不适合共享主机。
- `/admin` 与业务接口使用同一套 Bearer，没有后门。
- 不要把 `config.json` 里的口令提交到 git。

## 开发

```bash
go test ./...
go build -o codefree2api ./cmd/codefree2api
```
