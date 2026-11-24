# CodeFree Space 行数分普通和 spec

CodeFree Space 的 spec 行数和普通行数不是同一组键。2026-11-19 抓包里，数字员工采纳编辑的 `app` 是 `codefree-ide-spec`，`service` 是 `ide-digital-employee`，四条键是 `agent_gen_code_line`、`agent_gen_code_count`、`agent_accepted_code_line`、`agent_accepted_code_count`。两条行数共用一个 `reqId`，两条次数不带 `reqId`。`client.type` 同样是 `ide-win32`。抓到的 `subKey` 是当时工作区的数字员工名，不是产品常量，所以命令不写死它，缺省用客户端在没有名字时的 `Unknown`。`lines space` 发普通的一种，`lines space spec` 发 spec 的一种。一次命令只发一种。项目查询仍用 `clientType` `codefree-ide`。两种行数的正文都是 `text/plain;charset=UTF-8`。
