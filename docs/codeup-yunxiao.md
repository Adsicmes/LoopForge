# 阿里云云效 / Codeup 接入

LoopForge 原本只认两类代码托管：GitHub（内置 OAuth/PAT）与工蜂 Gongfeng（MCP）。本文件记录把
**阿里云云效 Codeup** 接成第三种 provider 的方式，全部凭据都是**云效个人访问令牌（PAT）**。

状态：后端已实现并有单元测试；Web UI 与工作项同步仍是待办（见文末）。

## 1. 身份与配置

| 概念 | 取值 |
| --- | --- |
| `provider` | `codeup` |
| 仓库标识（`gongfengProjectId` 列） | `codeup:{orgId}/{group?}/{repo}`，例如 `codeup:60de7a6852743a5162b5f957/backend/order` |
| Web 地址 | `https://codeup.aliyun.com/{orgId}/{group?}/{repo}` |
| 克隆地址 | 上面加 `.git`（HTTPS） |
| 凭据 | 用户个人设置里的 Git Token 槽位，存云效 PAT |
| 组织 ID | 仓库路径的第一段；中心版 OpenAPI 必须带 |

仓库表单里粘贴下面任一形式都会被识别（`internal/domain/codeup.go`）：

- `https://codeup.aliyun.com/{org}/{group}/{repo}.git`
- `https://codeup.aliyun.com/{org}/{group}/{repo}`（含 `/tree/…`、`/change/…` 等子页面）
- `git clone git@codeup.aliyun.com:{org}/{group}/{repo}.git`
- `codeup:{org}/{group}/{repo}`
- 裸路径 `{org}/{group}/{repo}`（仅当首段是 24 位十六进制组织 ID 时接受，避免把 `github.com/a/b` 误判为 Codeup）

## 2. 认证：个人访问令牌

1. 云效工作台 → 头像 → **个人设置 → 个人访问令牌 → 新建访问令牌**。
2. 权限按最小化勾选：**代码管理 → 合并请求 读写**、**代码库 只读**（要建 MR 必须给合并请求写）。
   需要导入需求时再给 **项目协作 → 工作项 只读**。
3. 设置到期时间。令牌只在创建时显示一次。
4. 把令牌粘进 LoopForge 的 **个人设置 → Git Token**，校验时带 `provider=codeup`。

实现要点：

- 令牌只放在 `x-yunxiao-token` 请求头里，**从不拼进 URL**，因此不会进 git remote、日志或运行快照。
- `git clone` / `push` 走 HTTPS，平台把令牌注入 userinfo（`https://{user}:{token}@codeup.aliyun.com/...`），
  复用既有 `gitwork.RewriteRemoteForToken`，不需要新代码路径；落库时仍会被 `StripURLCredentials` 抹掉。
- 令牌校验：`GET /oapi/v1/platform/user`（`GetUserByToken`）。返回空用户即判定无效。
  云效不回报令牌 scope，所以"能解析出用户"就算有效，真正的仓库权限在第一次实际调用时才会暴露。

## 3. API 映射

接入点：中心版 `https://openapi-rdc.aliyuncs.com`，`/oapi/v1/...`；Region 版自己的域名，
路径里去掉 `organizations/{orgId}`。用 `AGENTFLOW_CODEUP_API_BASE` 覆盖。

| LoopForge 动作 | 云效 OpenAPI | 备注 |
| --- | --- | --- |
| 令牌校验 | `GET /oapi/v1/platform/user` | 与组织无关 |
| 枚举仓库 | `GET /oapi/v1/codeup/organizations/{orgId}/repositories` | 支持 search / page / perPage |
| 解析仓库 | `GET .../repositories/{repoId 或 URL 编码全路径}` | 需要数字 `id` 做后续调用 |
| 找 MR | `GET .../organizations/{orgId}/changeRequests?projectIds=&state=` | **组织级**，不能按分支过滤 |
| 建 MR | `POST .../repositories/{id}/changeRequests` | 需要 source/target projectId，同库也要两次 |
| 合并 MR | `POST .../repositories/{id}/changeRequests/{localId}/merge` | body 必填 `mergeType` |
| 查 MR | `GET .../repositories/{id}/changeRequests/{localId}` | 合并是异步的，要轮询确认 |
| 导入需求 | `POST .../projex/organizations/{orgId}/workitems:search` | body 带 category/conditions，`page*perPage ≤ 10000` |

MR 状态到平台状态的映射（`internal/tracker/codeup.go`）：

- `UNDER_DEV` / `UNDER_REVIEW` / `TO_BE_MERGED` → `opened`
- `MERGED` → `merged`
- `CLOSED` → `closed`

## 4. 代码落点

| 文件 | 作用 |
| --- | --- |
| `internal/domain/codeup.go` | 地址解析、`codeup:` 身份、`codeup.aliyun.com` 常量；`ParseRepositoryURL` 已分派到它 |
| `internal/codeup/client.go` | OpenAPI 客户端（PAT、超时、响应上限、禁跟随重定向、错误中文化） |
| `internal/tracker/codeup.go` | 找/建/合并 MR、交付状态刷新；在 `merge_mr.go`、`mr_delivery_sync.go` 分派 |
| `internal/httpapi/catalog.go` | `provider` 白名单放开到 `codeup`，默认分支回落 `master` |
| `internal/httpapi/git_token_verify*.go` | 令牌校验按 `provider=codeup` 走云效 |

## 5. 已知限制

- **MR 列表只能按项目过滤**：平台先按 `projectIds` 拉一页（100 条），再在本地按源分支匹配。
  同一仓库长时间高频开 MR 时可能漏掉更早的分支，翻页可作为后续增强。
- **合并异步**：`merge` 接口返回 200 不代表已合并（可能要等评审或卡点检查）。实现里最多轮询 6 次 × 2s，
  超时会明确报错而不是假装成功。
- **没有合并时间戳**：change-request 详情不返回 merged_at，交付记录里的合并时间用轮询时刻近似。
- **令牌 scope 不可查**：只能靠"能否解析出用户"判断有效性。
- 云效的 MR 叫 change request；控制台里的"代码评审"就是这一层。

## 6. 待办（尚未实现）

- Web UI：仓库表单的 provider 选择器、令牌校验按仓库 provider 传参（后端接口已支持 `?provider=codeup`）。
- 仓库选择器：用 `ListRepositories` 做下拉，而不是让用户手填路径。
- 工作项导入：把 `SearchWorkItems` 接到 issue 导入流程（`Source: "codeup"`），对照 GitHub issue 导入的字段映射。
- 交付链接回写：在 MR 描述里带上 LoopForge run 链接。
- Webhook：Codeup 支持推送/评审事件回调，可用于替代轮询刷新交付状态。

## 7. 测试

```bash
go test ./internal/domain/ -run Codeup -count=1
go test ./internal/codeup/ -count=1
go test ./internal/tracker/ -run Codeup -count=1
```

测试用 `httptest` 起假 OpenAPI，覆盖：令牌头、组织级路径、全路径 URL 编码、建 MR 必填字段、
合并 `mergeType`、异步合明确认、404/401 报错文案、Region 版路径、缺令牌时提前失败。
