# 插件版本缺省查最新，也可以指定

`objects/aicode/plugin_version` 未登录返回 403，给不出版本。插件自己的检查是登录后 `GET /api/composqback/component/v1/detail?componentId=39999`，读 `latestVersion`。因此不写 `--plugin-version` 时用这个结果。写了就用写上的版本，不再查询。查询失败又没有写版本时，命令停住。
