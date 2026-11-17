# 行数上报失败就停住

插件或 CodeFree Space 的某一条行数响应不是成功，就不再发后面的请求，标准输出不写行数。已经送出的记录留在研发云。插件以 `optResult` 为 0 为成功。Space 以 `code` 为 0 或 `optResult` 为 0 为成功。
