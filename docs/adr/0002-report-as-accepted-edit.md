# 上报沿用一次已采纳的 edit

研发云的 `activity_record` 没有新增代码有效总行这个键。另造一个键不会进入现有的按月采纳行。因此一次上报沿用 codefree-o 采纳一次 `edit` 时的四条记录：`app`、`service`、`client.type` 都是 `codefree-o`，`subKey` 是 `edit`，两条行数都是生成代码采纳行数，两条次数都是 1。新增代码有效总行只留在标准输出。

四条请求的顺序与 2026-11-16 的抓包和客户端订阅顺序一致：`tools_gen_code_count`、`tools_gen_code_line`、`tools_gen_accepted_code_line`、`tools_gen_accepted_code_count`。任一条响应不是 `{"optResult":0}` 就停止，不再发送后面的请求。已经送出的记录不撤回。
