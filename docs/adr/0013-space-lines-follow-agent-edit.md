# CodeFree Space 行数沿用智能体编辑的四条记录

`codefree2api lines space` 按 2026-11-19 抓到的 CodeFree Space 1.5.1 普通模式采纳编辑上报：`ide_agent_gen_code_line`、`ide_agent_gen_code_count`、`ide_agent_accepted_code_line`、`ide_agent_accepted_code_count`。两条行数都是用户填写的生成代码采纳行数，两条次数都是 1。两条行数共用一个 `reqId`，两条次数不带 `reqId`。`app` 是 `codefree-ide`，`service` 是 `ide_agent`，`subKey` 是内置普通智能体 `Craft`。`client.type` 固定为 `ide-win32`，因为被模仿的是 Windows 桌面程序，Linux 上也不再发 `ide-linux`。正文 `Content-Type` 是 `text/plain;charset=UTF-8`，与抓到的行数请求一致。`client.version` 缺省是 `1.5.1`。新增代码有效总行只留在标准输出。spec 模式见 ADR 0026。
