# 一次进程只对接一个客户端

一次 `codefree2api` 进程在启动时选定 codefree-o、插件或 CodeFree Space，这次进程里的对话都走这个客户端的对话接口。不写时是 codefree-o；`--client plugin` 对接插件，`--client space` 对接 CodeFree Space。每个客户端的登录和上游都不一样，放进同一个进程里按请求切换会把这三套会话缠在一起。要同时用两个客户端，就起两个进程。
