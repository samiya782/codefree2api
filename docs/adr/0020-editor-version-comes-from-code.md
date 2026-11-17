# 插件的编辑器版本来自本机 code

插件请求里的 `client.version` 是编辑器版本，不是插件版本。`--editor-version` 优先。没写时用本机 `code --version`。两者都没有时命令停住。
