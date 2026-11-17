# CodeFree Space 的版本缺省是 1.5.1

`client.version` 缺省写 `1.5.1`，这是已经核对过请求形状的 `artifactVersion`。`--space-version` 改成指定版本。要查最新时，登录后走 `GET /api/acbackend/ideservice/v1/ide-releases`。`ide_release` 对象目录没有签名时列不出来。
