# CodeFree Space 反代走智能体对话接口

`--client space` 的对话发到 `POST /api/acbackend/codechat/v1/completions`，`subService` 是 `codefree-ide-agent`。补全用的 WebSocket 留在这轮外面。对外仍是现有的 OpenAI / Anthropic 接口。
