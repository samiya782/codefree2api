# 插件和 CodeFree Space 的身份来自这次登录页

这条决定由 [0025](0025-plugin-and-space-can-reuse-a-saved-login.md) 取代。

codefree-o 继续用本机已经登录的身份。插件和 CodeFree Space 本轮不读它们写在磁盘上的登录，命令行也不接收令牌。需要身份时，进程打开该客户端的研发云登录页，由用户完成登录，得到的会话只留在这次进程里。命令行令牌留到后续再加。
