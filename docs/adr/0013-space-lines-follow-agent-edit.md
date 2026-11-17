# CodeFree Space 行数沿用智能体编辑的四条记录

`codefree2api lines space` 按 CodeFree Space 1.5.1 在 `edit_file` 落地时的顺序上报：`ide_agent_gen_code_line`、`ide_agent_gen_code_count`、`ide_agent_accepted_code_line`、`ide_agent_accepted_code_count`。两条行数都是用户填写的生成代码采纳行数，两条次数都是 1。两条行数各带一个新的 `reqId`。`app` 是 `codefree-ide`，`service` 是 `ide_agent`，`subKey` 是 `Unknown`。`client.type` 在 Windows 上是 `ide-win32`，在 Linux 上是 `ide-linux`。`client.version` 是 `1.5.1`。新增代码有效总行只留在标准输出。
