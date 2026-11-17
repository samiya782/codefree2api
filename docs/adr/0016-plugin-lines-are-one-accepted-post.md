# 插件行数是一条只含采纳的请求

`codefree2api lines plugin` 发一条 `activity_record`。`activities` 先是 `agent_accepted_code_line`，再是 `agent_accepted_code_count`。行数是用户填写的生成代码采纳行数，次数是 1。`subKey` 是 `agent`，`app` 是 `aicode`，`service` 是 `agent`，`client.type` 是 `vscode`。`projectId` 是 0，`projectName` 为空。时间用插件的 `yyyy-M-d HH:m:s.SSS`。新增代码有效总行只留在标准输出。`pluginVersion` 在运行时决定，不写死安装包里的 `3.3.1`。
