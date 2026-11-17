# 代码量

codefree2api 统计一次改动的代码量，并把它上报到研发云。产生改动的 harness 可以不是 codefree-o。上报可以记在 codefree-o、插件或 CodeFree Space 上。这里只记录已经说定的词。

## Language

**插件**:
装在编辑器里的 CodeFree，现有 VS Code 与 JetBrains 两份安装包。
_Avoid_: plugin, 扩展, copilot

**CodeFree Space**:
单独安装的 CodeFree 桌面程序，有 Windows 和 macOS 两个版本。
_Avoid_: space, Space

**插件行数**:
记在插件上的一次行数上报。它和 CodeFree Space 行数是两套相近的请求。
_Avoid_: plugin lines

**插件版本**:
插件行数上报里的 pluginVersion。缺省是登录后查到的最新版本，也可以在命令里指定。
_Avoid_: 3.3.1, extension version

**Space 版本**:
CodeFree Space 上报里的程序版本。缺省是 1.5.1，也可以在命令里指定，或在登录后取最新。
_Avoid_: artifactVersion, 编辑器版本, 1.127.0

**CodeFree Space 行数**:
记在 CodeFree Space 上的一次行数上报。
_Avoid_: space 行数, space lines

**生成代码采纳行数**:
用户填写的整数，上报到研发云。codefree-o 自己改文件时，这个数等于该次改动新增的行与删除的行之和。本命令不从 diff 计算。
_Avoid_: lines, 采纳行, AI生成代码采纳行数

**新增代码有效总行**:
用户与生成代码采纳行数一起填写的整数。它只出现在标准输出里。
_Avoid_: 有效行, 新增行

**仓库**:
不给出 `--remote`、且生成代码采纳行数大于 0 时，上报所使用的 git 工作区，即当前目录所在的仓库。
_Avoid_: 项目, 代码中心项目

**远端**:
上报所记的 git 地址。命令可以给出一个或多个。不给出时，使用当前目录仓库的全部远端。
_Avoid_: remote url

**上报**:
一次统计结束后，把生成代码采纳行数交到研发云，记在这次使用的远端上。同一次统计再执行一次，就是另一次上报。新增代码有效总行留在本地输出里。生成代码采纳行数为 0 时不发生上报，命令也可以不在仓库里执行。
_Avoid_: 同步, 埋点

**登录**:
本机 codefree-o 已经可用的研发云身份。codefree-o 的上报使用这个身份，命令不另行接收令牌。
_Avoid_: token, api key

**会话**:
插件或 CodeFree Space 的研发云身份。它可以来自该客户端已经保存的登录，也可以来自为此打开的登录页。
_Avoid_: token, 登录
