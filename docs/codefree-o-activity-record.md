# codefree-o 代码行上报路径

观察对象是 WSL 上已安装的 `@srdcloud/codefree-o` 1.10.1。对话代理只把请求转到本机 `codefree-o serve`，不组装这条上报。`codefree2api lines` 按这里实测到的四条记录自行上报。下面是客户端从改文件到 `POST /api/acbackend/statserver/v1/activity_record` 的路径，以及 2026-11-16 在测试克隆上的实测。

测试克隆是 `https://yuzg2@code.srdcloud.cn/DICT-YCG/codefree_crazy`，本地目录 `/tmp/codefree_crazy`。二进制里这条 URL 只出现一次。按月汇总、有效行过滤、以及「必须是代码中心克隆」的判定，都不在这条客户端路径上。客户端每次只带本地时间和全部 git remote。

## 1. 调用链

```text
write / edit / apply_patch
  → 行数
  → 总线 codefree.metric.tool_gen_planned
  → 权限 ask
  → 总线 codefree.metric.tool_gen_accepted
  → 订阅者 eD / o1
  → Zo.recordMetric
  → POST {runtime.baseUrl}/api/acbackend/statserver/v1/activity_record
```

工具实现在 bunfs 块 `chunk-y92c0sw1.js`，订阅和上传在 `chunk-mvas63fc.js` 的类 `Zo`。

1. `write` 和 `edit` 用 `gA(old, new)` 算行。`gA` 遍历 `diffLines` 的结果，`added` 累加 `count`，`removed` 累加 `count`，返回 `{additions, deletions}`。上报用的 `lines` 是 `additions + deletions`。
2. `edit` 在 diff 之前先把 `\r\n` 换成 `\n`。`write` 对不存在的文件把旧文本当作空字符串。
3. `callID` 为真时，先发 `ToolGenPlanned`，属性是 `tool`、`callID`、`lines`。
4. 然后 `ask({permission:"edit", always:["*"]})`。ask 抛出（拒绝）时，采纳事件不会发。
5. ask 返回并且 `callID` 仍为真时，发 `ToolGenAccepted`，`lines` 与计划相同。`apply_patch` 还要求变更列表长度大于 0。
6. 采纳事件在 `writeWithDirs` 之前发出。后面的写盘失败不会撤回已经发出的 POST。`recordMetric` 也不等待业务方。
7. 订阅者 `eD` 处理计划事件：先 `tools_gen_code_count`，`subKey` 是工具名，`count` 为 1；`lines > 0` 时再发 `tools_gen_code_line`。
8. 订阅者 `o1` 处理采纳事件：`lines > 0` 时先发 `tools_gen_accepted_code_line`，然后总是发 `tools_gen_accepted_code_count`，`count` 为 1。
9. 同一个事件 id 进一个 512 格的环。重复 id 被丢掉，所以同一次工具调用不会打两遍。
10. `O()` 调用 `Zo.recordMetric(key, subKey, count)`。没有 `userId` 时 `build_request` 直接返回，不上传。`environment` 模式要求十进制安全整数，其他模式 `parseInt`。
11. `get_git_urls` 在当前 worktree（取不到则 `process.cwd()`）执行 `git remote`，再对每个名字执行 `git remote get-url`，用 Set 去重。失败得到 `[]`。客户端不过滤 host。
12. `time` 由 `hD(new Date)` 格式化成本地时间 `YYYY-MM-DD HH:mm:ss.SSS`。没有月份字段。
13. `x_` 用全局 `fetch` 发 POST，`Content-Type: application/json`，60 秒中止。`!j.ok` 或异常只打警告「记录研发云 metric 失败」。

`apply_patch` 的新增和更新走同一套 `diffLines`。删除不走 `diffLines`：`deletions = oldText.split("\n").length`，`additions = 0`。文件末尾有换行时，这个长度比行数多 1。多个 hunk 先把 `additions + deletions` 加总，再发一条计划事件和一条采纳事件，`tool` 为 `apply_patch`。

`bash` 只记 `shell_count = 1`，不进代码行。助手消息里的围栏代码记 `chat_gen_code_count` 和 `chat_gen_code_line`，那是对话里的代码块，不是落盘采纳行。

## 2. 行数规则

| 操作 | 算法 | 上报的 lines |
| --- | --- | --- |
| write 新文件 | `diffLines("", 新文本)` | 新增行 token 数 |
| edit / write 覆盖 | `diffLines(旧, 新)` | 新增 token + 删除 token |
| apply_patch 新增、更新 | 同上 | 各文件相加 |
| apply_patch 删除 | `split("\n").length` | 只计删除 |

行 token 按 jsdiff 的 `diffLines` 默认选项切：换行符贴在上一行上。空行计入。注释不剔除。一次只改一行时，计划和采纳的 `lines` 都是 2。

工具结果里的 `filediff.additions` / `filediff.deletions` 是分开的。指标 POST 里的 `count` 是两者之和。

## 3. 哪些工具会露出

`ToolRegistry.tools` 用模型 id 做分流：

```text
gpt = modelID 含有 "gpt-" 且不含 "oss" 且不含 "gpt-4"
gpt 为真：露出 apply_patch，藏起 edit 和 write
gpt 为假：露出 edit 和 write，藏起 apply_patch
```

2026-11-16 本机 `codefree-o models` 只有这 6 个，都不含 `gpt-`：

- `codefree/DeepSeek-V4-Flash-0731-TokenPlan`
- `codefree/GLM-5.1-AH`
- `codefree/GLM-5.2-TokenPlan`
- `codefree/GLM-5.3-TokenPlan`
- `codefree/Qwen3.8-27B-TokenPlan`
- `codefree/TeleXing4.0-29B-A4B`

因此这一环境下的增、改走 `write` / `edit`。删除文件的指标只可能从 `apply_patch` 出来。指定 `codefree/gpt-5.2` 时客户端报 `Model not found`，删除请求没有发出。

## 4. 请求形状

```http
POST /api/acbackend/statserver/v1/activity_record HTTP/1.1
Host: www.srdcloud.cn
Content-Type: application/json
User-Agent: opencode/1.10.1
projectId: 0
userId: <整数>
```

签名用的 `X-Cf-Token`、`X-Cf-Signature`、`X-Cf-Nonce`、`X-Cf-AppId` 不写入本文。实测里请求头 `userId` 和正文 `userId` 是同一个十进制整数。

```json
{
  "userId": 0,
  "app": "codefree-o",
  "service": "codefree-o",
  "time": "YYYY-MM-DD HH:mm:ss.SSS",
  "client": {
    "type": "codefree-o",
    "version": "1.10.1",
    "gitUrls": ["<git remote get-url 的结果>"]
  },
  "activities": [
    {
      "key": "<下表之一>",
      "subKey": "write | edit | apply_patch | 空字符串",
      "value": { "count": 1 }
    }
  ]
}
```

一次工具调用在 `lines > 0` 且权限通过时发出 4 个 POST，每个 POST 的 `activities` 只有一个元素：

| 顺序 | key | count |
| --- | --- | --- |
| 计划 | `tools_gen_code_count` | 1 |
| 计划 | `tools_gen_code_line` | lines |
| 采纳 | `tools_gen_accepted_code_line` | 同一个 lines |
| 采纳 | `tools_gen_accepted_code_count` | 1 |

会话开始还会发 `chat_count`，`subKey` 为 `no_at`，`count` 为 1。那不是代码行。

成功时响应是 `HTTP 200`，正文 `{"optResult":0}`。

测试克隆的 remote 是 `https://yuzg2@code.srdcloud.cn/DICT-YCG/codefree_crazy`。实测 `gitUrls` 数组里只有这一条。

## 5. 第一次实测（2026-11-16）

模型 `codefree/GLM-5.2-TokenPlan`，目录 `/tmp/codefree_crazy`，`--pure --auto`。流量经本机 Burp `127.0.0.1:8080`，拦截关闭。

`write` 创建 `probe/added.py`，4 个行 token，全部是新增。独立重算 `additions=4`，`deletions=0`。

`edit` 把 `probe/base.py` 的 `return left + right` 改成 `return left + right  # sum`。工具元数据 `additions=1`，`deletions=1`。独立重算合计 2。

| time（本地） | key | subKey | count | 与重算 |
| --- | --- | --- | --- | --- |
| 2026-11-16 03:29:57.647 | tools_gen_code_count | write | 1 | 一次 write |
| 2026-11-16 03:29:57.647 | tools_gen_code_line | write | 4 | 4 |
| 2026-11-16 03:29:57.648 | tools_gen_accepted_code_line | write | 4 | 4 |
| 2026-11-16 03:29:57.648 | tools_gen_accepted_code_count | write | 1 | 一次 write |
| 2026-11-16 03:29:58.501 | tools_gen_code_count | edit | 1 | 一次 edit |
| 2026-11-16 03:29:58.501 | tools_gen_code_line | edit | 2 | 2 |
| 2026-11-16 03:29:58.502 | tools_gen_accepted_code_line | edit | 2 | 2 |
| 2026-11-16 03:29:58.502 | tools_gen_accepted_code_count | edit | 1 | 一次 edit |

同一次会话开始还有 `chat_count` / `no_at` / `1`，时间 `2026-11-16 03:26:27.677`。请求头 `projectId` 是 `0`，`gitUrls` 是上面的代码中心地址。本地 `time` 比响应 `Date` 的 GMT 早 8 小时。8 个代码行 POST 的响应都是 `{"optResult":0}`。

`probe/gone.py` 仍在。Burp 历史里没有 `subKey` 为 `apply_patch` 的请求。按第 2 节，若删除发生，这个文件的 `lines` 应是 `split("\n").length = 5`，而不是 `diffLines` 对空文本的 4。

测试克隆上的对应提交是 `d073525`，作者 `yuzg2 <yuzg2@chinatelecom.cn>`。

## 6. 重复验证要核对的点

每次新会话、每个新文件，都要看到：

1. 一个 `chat_count`。
2. 每个成功的 `write`：4 个 POST，`subKey` 为 `write`，行数等于该文件对空文本的 `diffLines` 合计。
3. 每个成功的 `edit`：4 个 POST，`subKey` 为 `edit`，行数等于旧文本到新文本的 `diffLines` 合计。
4. 计划行数等于采纳行数。
5. `gitUrls`、`version`、URL、`activities` 只有一个元素、响应 `optResult` 为 0。
6. 正文仍然没有月份字段。

下面三次的预期在实验前写好。模型若改了别的内容，以落盘字节的重算为准。

| 轮次 | 文件 | write 预期 lines | edit 预期 lines |
| --- | --- | --- | --- |
| 1 | `probe/rep1.py` | 6 | 2 |
| 2 | `probe/rep2.py` | 9 | 4 |
| 3 | `probe/rep3.py` | 3 | 2 |

第 1 轮把 `return 11` 改成 `return 11  # rep1`。第 2 轮把 `return 21` 和 `return 22` 两行都加上注释。第 3 轮把 `return 31` 改成 `return 31  # rep3`。

## 7. 重复验证结果

2026-11-16 用同一个模型 `codefree/GLM-5.2-TokenPlan`、同一套 `--pure --auto`，在三次新会话里各做一次 `write` 和一次 `edit`。三次落盘字节都与实验前写好的文本一致。Burp 里这三次的代码行 POST 都是 `HTTP 200`、`{"optResult":0}`。`userId` 与第一次实测相同，`version` 仍是 `1.10.1`，`gitUrls` 仍只有代码中心这一条，正文仍然没有月份字段。计划行数每次都等于采纳行数。

| 轮次 | 会话开始 | write lines | edit lines | 与第 6 节 |
| --- | --- | --- | --- | --- |
| 1 `probe/rep1.py` | 03:41:46 `chat_count` | 6 | 2 | 一致 |
| 2 `probe/rep2.py` | 03:43:28 `chat_count` | 9 | 4 | 一致 |
| 3 `probe/rep3.py` | 03:45:43 `chat_count` | 3 | 2 | 一致 |

每一轮的 8 个代码行 POST 如下。`subKey` 为 `write` 或 `edit`，`count` 为该次的值。时间是正文里的本地 `time`。

第 1 轮：

| time | key | count |
| --- | --- | --- |
| 03:43:09.924 | tools_gen_code_count / write | 1 |
| 03:43:09.924 | tools_gen_code_line / write | 6 |
| 03:43:09.925 | tools_gen_accepted_code_line / write | 6 |
| 03:43:09.925 | tools_gen_accepted_code_count / write | 1 |
| 03:43:18.988 | tools_gen_code_count / edit | 1 |
| 03:43:18.988 | tools_gen_code_line / edit | 2 |
| 03:43:18.988 | tools_gen_accepted_code_line / edit | 2 |
| 03:43:18.995 | tools_gen_accepted_code_count / edit | 1 |

第 2 轮：

| time | key | count |
| --- | --- | --- |
| 03:45:19.468 | tools_gen_code_count / write | 1 |
| 03:45:19.469 | tools_gen_code_line / write | 9 |
| 03:45:19.469 | tools_gen_accepted_code_line / write | 9 |
| 03:45:19.469 | tools_gen_accepted_code_count / write | 1 |
| 03:45:33.560 | tools_gen_code_count / edit | 1 |
| 03:45:33.561 | tools_gen_code_line / edit | 4 |
| 03:45:33.561 | tools_gen_accepted_code_line / edit | 4 |
| 03:45:33.561 | tools_gen_accepted_code_count / edit | 1 |

第 3 轮：

| time | key | count |
| --- | --- | --- |
| 03:47:30.692 | tools_gen_code_count / write | 1 |
| 03:47:30.692 | tools_gen_code_line / write | 3 |
| 03:47:30.692 | tools_gen_accepted_code_line / write | 3 |
| 03:47:30.692 | tools_gen_accepted_code_count / write | 1 |
| 03:47:39.845 | tools_gen_code_count / edit | 1 |
| 03:47:39.846 | tools_gen_code_line / edit | 2 |
| 03:47:39.846 | tools_gen_accepted_code_line / edit | 2 |
| 03:47:39.846 | tools_gen_accepted_code_count / edit | 1 |

三次把上报行数从 6、9、3 和对应的编辑 2、4、2 各走了一遍，计数跟着 diff 变，请求形状不变。删除文件的 `apply_patch` 路径这次仍没有模型可以触发，第 3 节的门禁保持原样。
