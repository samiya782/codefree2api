# Space 对话是 SSE，插件对话是 WebSocket

`--client space` 的对话发到 `POST /api/acbackend/codechat/v1/completions`，响应按 SSE 读取：`data:` 后是 JSON，结束行是 `data: [DONE]`。`--client plugin` 的对话发到 `wss://{host}/websocket/peerAppgw`，消息名是 `CodeChatRequest`。插件不另接那条 SSE，Space 不另接补全用的 WebSocket。
