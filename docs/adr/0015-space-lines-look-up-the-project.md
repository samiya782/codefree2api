# CodeFree Space 行数先按远端查项目

`lines space` 把全部远端交给 `POST /api/acbackend/usermanager/v1/users/project`，再用返回的项目编号和名称填写上报。查不到时 `projectId` 为 0、`projectName` 为空，四条行数请求仍然发送。`gitUrls` 是全部远端，`gitUrl` 是 `.git/config` 里第一条 `url`。`client.version` 是 `1.5.1`。
