# 插件和 CodeFree Space 的实测

2026-11-16 在 WSL 上用本仓库编译的 `codefree2api` 做了行数上报和本地代理。远端是测试仓库 `https://yuzg2@code.srdcloud.cn/DICT-YCG/codefree_crazy`。本地克隆当时不在，上报用 `--remote` 写出这条地址。令牌、登录页地址和签名不写入本文。

`go test ./...` 在这次实测之后通过。

## 用法

一次进程只选一个客户端。身份用 `login` 留下，或沿用该客户端已经写下且尚未过期的登录。

```bash
codefree2api login --client space
codefree2api login --client plugin

codefree2api lines space --remote https://yuzg2@code.srdcloud.cn/DICT-YCG/codefree_crazy 8 3
codefree2api lines plugin --remote https://yuzg2@code.srdcloud.cn/DICT-YCG/codefree_crazy --editor-version 1.146.0 8 3

codefree2api -client space -port 8765
codefree2api -client plugin -port 8766 -editor-version 1.146.0
```

两个整数仍是生成代码采纳行数和新增代码有效总行。采纳行数为 0 时不上报，也不登录。WSL 里的 `code` 会去安装 Linux 上的 VS Code Server，所以插件这次用 `--editor-version` 写出 Windows 安装里的 `1.146.0`。浏览器打不开时，标准错误仍打印 `登录页：` 和完整地址。

代理只监听 `127.0.0.1`。下面的对话都打到这个地址。

## 行数

采纳行数为 0 的两次都只打印两行并退出 0，发生在登录之前：

```text
生成代码采纳行数 0
新增代码有效总行 1
```

已保存的 CodeFree Space 访问令牌当时已经过期。第一轮正数上报打开了登录页，浏览器里已有的研发云会话把登录走完，命令随后打印了两行计数。插件在此之前已经用 `codefree2api login --client plugin` 留下身份。三轮正数上报的标准输出与传入的整数一致，每次退出码都是 0。失败时命令不会打印这两行。

| 轮次 | 生成代码采纳行数 | 新增代码有效总行 | CodeFree Space | 插件 |
| --- | --- | --- | --- | --- |
| 1 | 6 | 2 | 20:39:49 退出 0 | 20:39:50 退出 0 |
| 2 | 9 | 4 | 20:39:50 退出 0 | 20:39:51 退出 0 |
| 3 | 3 | 2 | 20:39:50 退出 0 | 20:39:51 退出 0 |

时间是本地时间。命令在 Linux 上执行。按实现，CodeFree Space 的 `client.type` 是 `ide-linux`，插件是 `vscode`。这次没有另存请求正文。

上报之后用同一条远端再查了一次项目和版本，都是 HTTP 200：

| 查询 | 结果 |
| --- | --- |
| CodeFree Space `ide-releases?onlyLast=true` | 版本 `1.5.1` |
| 该远端的项目 | `project.id` 35842，`project.name` `DICT-YCG` |
| 插件 `componentId=39999` 的 `latestVersion` | `1.3.8` |

三轮都没有写 `--space-version` 或 `--plugin-version`。插件命令没有停在「没有插件版本」。编辑器版本是显式传入的 `1.146.0`。

## 代理

两个进程分别是 `-client space -port 8765` 和 `-client plugin -port 8766 -editor-version 1.146.0`。`GET /health` 都是 `{"status":"ok"}`。`GET /v1/models` 都返回两个 id：`Auto`、`Qwen3.8-27B`。

下面用 `Qwen3.8-27B`。提示语是 “Reply with the single word pong.”。非流式 Chat Completions 各三轮，流式、Anthropic Messages、Responses 各一轮。回复文本都是 `pong`。

| 调用 | CodeFree Space | 插件 |
| --- | --- | --- |
| Chat Completions ×3 | 200，`pong` | 200，`pong` |
| `stream: true` | 200，`pong` | 200，`pong` |
| `POST /v1/messages` | 200，`pong` | 200，`pong` |
| `POST /v1/responses` | 200，`pong` | 200，`pong` |
| `previous_response_id` | 400 `unknown previous_response_id` | 400 `unknown previous_response_id` |
| 要求调用 `ping` 工具 | 200，`tool_calls` 名为 `ping` | 200，正文说明不能代为执行 ping |

`Auto` 再各做一轮非流式对话，回复也是 `pong`。

CodeFree Space 的补全是 SSE，字段是 `id`、`model`、`choices.delta.content`、`choices.delta.tool_calls`、`finish_reason` 和 `usage`。其中的 `id` 是单次响应的 id，没有可交给下一次请求的 session。插件走 WebSocket。因此这两个客户端上，Responses 返回的 id 不能用 `previous_response_id` 续写；未知 id 按原有接口返回 400。codefree-o 仍用它自己的上游 session 续写。

这两轮代理结束时，`GET /api/stats` 都是 `requests` 8、`ok` 8、`errors` 0。`previous_response_id` 的 400 发生在联系上游之前，不计入这 8 次。之后的 `Auto` 一轮是另一次成功调用。服务日志里没有失败行。
