# 阿里云云效（Codeup + Projex）接入方案

> 状态：P0–P2 核心已实现，P3 待评估  
> 基线：`main@00d4f17`（2026-11-16）  
> 目标读者：负责 `internal/domain`、`internal/tracker`、`internal/engine`、`internal/httpapi`、`internal/repo` 与前端仓库配置的开发者  
> 结论先行：**以云效 OpenAPI 作为平台自身的确定性同步通道，以官方云效 MCP 作为 Agent 的补充工具入口；先打通 Codeup 仓库 + MR 与 Projex 工作项，再评估 Flow 流水线。**
>
> 实施记录：P0（provider/URL/MR 解析、OpenAPI 客户端、云效 PAT 与脱敏）和 P1/P2 的核心后端（Codeup 仓库校验/MR 交付、Projex 拉取/创建/评论、来源过滤）已落地；Codeup Git 传输限定为 SSH，PAT 只走 OpenAPI。前端仓库表单、个人凭据、来源标签与同步配置已接入口；真实云效环境的域名、权限点和 Projex URL 模板仍需按第 9 节核对。

---

## 1. 需求与边界

“给 LoopForge 添加阿里云云效支持”在本方案中落实为四条可验收的能力：

1. **Codeup 仓库**：在项目里绑定云效代码库，能够检出、创建/复用工作分支、推送，并在对话或 SOP 交付时创建/合并 MR。
2. **MR 生命周期**：按工作分支找到 MR，记录 MR 链接、目标分支与合并状态；交付分析能识别“已合入默认分支 / 已合入基线 / 仍开启 / 已关闭”。
3. **Projex 工作项**：拉取需求/缺陷/任务，保留本地状态与澄清结果；从本地需求创建云效工作项；把澄清摘要写回工作项评论；定时任务可按云效来源拉取并派发 SOP。
4. **配置与凭据**：个人配置云效访问令牌；项目配置 Projex 空间与工作项类型；平台配置云效 API 域名与组织；UI、文档、错误提示与测试同步更新。

**MVP 不做**（避免一次改动过大）：

- 云效 Flow 流水线的触发、日志读取与卡点联动；MVP 只保留仓库上的流水线 URL 展示位。
- AppStack、Packages、Testhub 的写操作。
- 云效 OAuth 登录/授权；MVP 使用个人访问令牌（PAT）。
- 一个平台实例同时对接多个云效组织；MVP 假设一个实例一个组织，组织级配置放在平台配置中。
- Codeup HTTPS 克隆账号 + 密码的双凭据体系；MVP 的 Git 传输优先走 SSH，PAT 只用于 OpenAPI。HTTPS 方案放到 P3。

---

## 2. 现状事实（代码证据）

### 2.1 当前 provider / source 模型

| 维度 | 现有取值 | 代码位置 |
| --- | --- | --- |
| 仓库 provider | `github`、`gongfeng`、`ssh` | `internal/domain/project.go:66-87`、`internal/httpapi/catalog.go:233-239` |
| 需求 source | `local`、`gongfeng`、`tapd`（另有 GitHub Issue 拉取） | `internal/domain/issue.go:50`、`internal/domain/sync.go:171`、`internal/tracker/service.go:293-414` |
| MR 身份 | GitHub 用 `github:owner/repo`；工蜂用 `group/repo`；MR 记录用 `MRProject + MRIID` | `internal/domain/delivery_mr.go:25-35`、`internal/domain/mr.go:13-58` |
| 外部工作项身份 | `ExternalRef.Source/ExternalID/ExternalIID/ExternalURL/ExternalState/ExternalType` | `internal/domain/issue.go:181-195` |
| 同步凭据 | 用户级 `GitToken`，工蜂 OAuth 刷新；项目 Owner 的 MCP Headers | `internal/domain/user.go:33-38`、`internal/repo/gongfeng_credential.go:31-84`、`internal/tracker/catalog.go:79-110` |

仓库的 `GongfengProjectID` 是历史 wire 字段，但 `docs/architecture.md:19` 已明确它承载远端仓库身份（GitHub 用 `github:` 前缀）。本方案沿用这个兼容策略，不为云效新造同义字段。

### 2.2 关键调用链

**仓库登记**

- 后端入口：`internal/httpapi/catalog.go:74-258`。
- URL 解析：`internal/domain/public_repository.go:11-42`（GitHub）与 `internal/domain/project.go:182+`（工蜂）。
- SSH 判定：`legacySSH` 只把 `git.example.com` 与 `github.com` 视为“带托管 API 的 SSH 地址”，其余 SSH 地址会落到 `provider=ssh`（无托管 API）。

**SOP 交付**

- 交付 MR 解析：`internal/engine/lifecycle.go:165-180` → `internal/domain/delivery_mr.go:169-237`。
- 合并 MR：`internal/tracker/merge_mr.go:20-115`，按 `github:` 前缀分流，其余走工蜂 MCP。
- MR 状态刷新：`internal/tracker/mr_delivery_sync.go:83-148`，同样按 `github:` 前缀分流。
- 交付分析：`internal/repo/delivery.go` 中的 artifact 扫描 SQL 使用 `%merge_requests/%`（约 732 行与 1427 行）；`internal/repo/analytics_business_merge.go:30` 也有同类条件。

**对话改代码后提交 MR**

- `internal/httpapi/assistant_workspace_mr.go:71-292`：先推送工作分支，再按 GitHub / 工蜂分别调用 `createGitHubPR` 或 `createGongfengMR`。
- 只认 `githubRepoPath` / `gongfengProjectPath` 两种远端身份（同文件 294-341 行）。

**外部工作项同步**

- 全量拉取：`internal/tracker/service.go:293-363`。
- 定时任务拉取：`internal/tracker/service.go:365-414`。
- 工蜂 Issue 映射：`internal/tracker/service.go:418-536`。
- 推送/写回：`internal/tracker/service.go:558-687`。
- 外部身份去重：`internal/tracker/service.go:1902-1957`。
- 本地状态保留：`internal/tracker/service.go:1849-1900`（`applyExistingIssue`）。
- 默认 SOP 选择：`internal/repo/catalog.go:798-839`（`DefaultWorkflowID` / `isRequirement` 只识别工蜂 Issue 与 TAPD 需求）。

**Git 凭据与 SSH**

- Token 解析：`internal/gitwork/auth.go:28-48`，只识别 `private:` 与 `oauth2:` 两种用户名前缀。
- HTTPS 重写：同文件 78-113 行，只重写 `git.example.com` 系列地址。
- SSH：`internal/gitwork/ssh.go` 支持任意主机的个人 SSH key；`RunnerRepository.UseSSH` 已经存在（`internal/domain/runner.go`）。
- Runner 分发：`internal/engine/runner_dispatch.go:334-381` 目前按 `provider == ssh` 设置 `UseSSH`。

### 2.3 为什么不能只加一个 `if`

云效接入至少会碰到以下硬编码点；只加一个 provider 字符串会留下“仓库能绑定、交付不工作”的半成品：

| 位置 | 现状 | 需要的变化 |
| --- | --- | --- |
| `internal/domain/public_repository.go` | 只识别 github.com | 增加 Codeup/Region 域名解析，返回 `provider=yunxiao` |
| `internal/httpapi/catalog.go` | `legacySSH` 只认工蜂/GitHub；provider 只允许 github/gongfeng | 增加云效 SSH 特判与 `yunxiao` provider |
| `internal/domain/delivery_mr.go` | `GongfengMRURL` 只生成 GitHub/工蜂 URL | 增加 `codeup:` 身份与 `/change/{id}` URL |
| `internal/domain/mr.go` | `ParseGongfengMRs` 只认工蜂/GitHub | 增加 Codeup MR URL，并统一为通用 MR 解析 |
| `internal/tracker/merge_mr.go` | 只分流 github / 工蜂 | 增加云效 MR 适配器 |
| `internal/tracker/mr_delivery_sync.go` | 只分流 github / 工蜂 | 增加云效状态刷新 |
| `internal/tracker/service.go` | Pull/Push/定时任务只认 gongfeng/tapd | 增加 yunxiao 分支与池模型 |
| `internal/httpapi/assistant_workspace_mr.go` | 只创建 GitHub PR / 工蜂 MR | 增加云效 MR 创建 |
| `internal/repo/delivery.go` | artifact SQL 只匹配 `merge_requests` | 增加 Codeup `/change/` 形态 |
| `internal/engine/*` | SSH 判定只认 `provider=ssh`；HTTPS token 只重写工蜂 | 将“是否 SSH”抽成 helper，Codeup SSH 走 SSH key |
| `internal/repo/users.go` / `models.go` | 只有 GitToken / 工蜂 OAuth | 增加云效 PAT 字段与校验 |
| `web` / `chatbot` | 只认 GitHub / 工蜂 / 通用 SSH | 增加云效仓库解析、来源标签、配置表单 |
| `docs/api/openapi.yaml` | enum 只含 github/gongfeng/tapd | 增加 yunxiao 并补字段 |

---

## 3. 方案选型

### 3.1 云效 OpenAPI 还是官方 MCP

| 方案 | 优点 | 风险 | 结论 |
| --- | --- | --- | --- |
| 只用官方 MCP | 复用现有 `internal/tracker` 的 MCP 客户端与项目级 Headers；官方维护；支持 OAuth | 工具名/参数可能随版本变化；当前官方工具列表没有“合并 MR”工具；交付状态需要强类型响应，MCP 文本解析脆弱 | 不作为平台同步主路径 |
| 只用 OpenAPI | 路径、状态、分页、错误码稳定；有 `MergeChangeRequest`；可用 `x-yunxiao-token` 直接认证 | 需要自写客户端与类型；要处理中心站/Region 差异 | **作为主路径** |
| OpenAPI + MCP 双通道 | 平台同步确定性强；Agent 仍可用云效官方工具查代码/工作项/流水线 | 需要维护两套配置入口 | **推荐**：OpenAPI 负责平台闭环，MCP 作为可选能力 |

官方 MCP 证据：`alibabacloud-devops-mcp-server` 提供 `create_change_request`、`get_change_request`、`list_change_request`、`search_workitems`、`create_work_item` 等工具，但 README 的工具清单没有 `merge_change_request`；而 OpenAPI 有 `MergeChangeRequest`。因此“自动合并/交付闭环”不能依赖 MCP。

### 3.2 认证方式

- **MVP 使用个人访问令牌（PAT）**。令牌由用户在个人设置中创建，至少授予：
  - 代码管理：仓库、分支、合并请求的读写（需要创建/合并 MR 时）；
  - 项目协作：工作项、工作项评论的读写（需要同步/创建/评论时）；
  - 组织管理：组织/成员只读（用于令牌校验与负责人解析）。
- 服务端不保存云效账号密码；不把 PAT 写进仓库 JSON、Run 快照、日志或 URL。
- 令牌校验：中心站调用 `GET /oapi/v1/platform/organizations`；Region 站没有该端点时，退化为调用已绑定仓库的 `GetRepository` 或工作项搜索，用只读请求验证权限。校验结果只返回组织名/登录名/缺失权限，不回显令牌。
- OAuth 2.0 放到 P3；官方 MCP 支持 OAuth，但平台自身的 OpenAPI 同步仍以 PAT 为 MVP。

### 3.3 Git 传输

- **MVP 推荐 Codeup SSH**：`git@codeup.aliyun.com:org/repo.git` 或 `ssh://git@codeup.aliyun.com/org/repo.git`。平台已有个人 SSH key、host key 校验与 Runner `UseSSH` 机制，改动面最小。
- Codeup PAT 用于 OpenAPI；不要求用户再提供 HTTPS 克隆账号/密码。
- **P3 可选 HTTPS**：需要引入“按仓库选择凭据”的多主机 Git 重写（当前 `gitwork.SandboxEnv` 只重写工蜂域名，且 `RunnerRepository` 只带一个 token）。这会涉及 `internal/gitwork`、`internal/runtime`、`internal/runner` 与 `RunCredentials`，不应和 MVP 混在一起。

### 3.4 多组织与多 Region

- MVP 一个平台实例对接一个云效组织；`YunxiaoAPIBase` 与 `YunxiaoOrganizationID` 放在 `SyncConfig`（平台级）。
- 中心站默认 `https://openapi-rdc.aliyuncs.com`；Region 站配置为 `https://<org>.devops.aliyuncs.com`。
- 仓库身份只存 `codeup:{org}/{path}`，不把域名塞进身份；MR URL 的默认 Web base 用 `AGENTFLOW_YUNXIAO_WEB_BASE`（默认 `https://codeup.aliyun.com`）解析，Region 部署显式覆盖。
- 未来需要多组织时，再把组织 ID/凭据下沉到项目级；当前不为未来预留会立即腐化的字段。

---

## 4. 总体设计

```text
┌──────────────────────────────────────────────────────────────┐
│ UI：项目设置 / 个人设置 / 需求列表 / 同步配置                  │
└───────────────┬──────────────────────────────────────────────┘
                │ REST
┌───────────────▼──────────────────────────────────────────────┐
│ httpapi：catalog / sync / issues / assistant_workspace_mr     │
│  - provider=yunxiao 的登记、校验、提交 MR、同步触发            │
└───────────────┬──────────────────────────────────────────────┘
                │
┌───────────────▼──────────────────────────────────────────────┐
│ tracker.Service + internal/yunxiao                            │
│  - Codeup：仓库解析、MR 查找/创建/合并/状态                    │
│  - Projex：工作项搜索/创建/评论/状态映射                       │
│  - 凭据：项目 Owner / 责任人 / 请求人的 PAT                    │
└───────────────┬──────────────────────────────────────────────┘
                │ HTTPS + x-yunxiao-token
┌───────────────▼──────────────────────────────────────────────┐
│ 云效 OpenAPI（中心站或 Region 站）                             │
│  /oapi/v1/codeup/...  /oapi/v1/projex/...                     │
└──────────────────────────────────────────────────────────────┘

Git 传输：SSH（个人 key，现有 gitwork/ssh.go）→ Codeup
可选：官方托管 MCP `https://openapi-rdc.aliyuncs.com/ai/mcp`
      仅给 Agent 做代码/工作项/流水线查询，不承担平台交付状态。
```

### 4.1 身份约定

| 对象 | 约定 | 说明 |
| --- | --- | --- |
| `Repository.Provider` | `yunxiao` | UI 显示“云效 Codeup” |
| `Repository.GongfengProjectID` | `codeup:{org}/{path}` | 复用历史 wire 字段，避免新增同义字段；`org` 是 Codeup 组织路径，`path` 是仓库完整路径 |
| `Repository.YunxiaoRepoID` | Codeup 数字仓库 ID | 首次校验时从 `GetRepository` 解析并缓存；创建 MR 的 `sourceProjectId/targetProjectId` 必须用它 |
| `Project.YunxiaoSpaceID` | Projex 空间 ID | 工作项搜索/创建必需 |
| `Project.YunxiaoWorkItemTypes` | `{"Req":"...","Bug":"...","Task":"..."}` | 工作项类型 ID；按本地 Kind/来源选择 |
| `Project.YunxiaoStatusMap` | 外部状态 ID/名称 → 本地状态 | Projex 工作流可自定义，不能只靠硬编码中文 |
| `MRProject` | `codeup:{org}/{path}` | 与仓库身份一致，便于按仓库反查数字 ID |
| `MRIID` | Codeup `localId` | 页面 URL 与服务端路径都用它 |
| `ExternalRef.Source` | `yunxiao` | 与 `gongfeng`/`tapd` 并列 |
| `ExternalRef.ExternalID` | 工作项 ID（字符串） | Projex ID 不是整数，不能用 `ExternalIID` |
| `ExternalRef.ExternalType` | `Req`/`Bug`/`Task` | 用于评论与 URL 生成 |
| `ExternalRef.ExternalState` | 原始状态 ID 或 `nameEn` | 外部关闭判断使用它，不依赖本地 `Status` |

`codeup:` 前缀必须被工蜂适配器拒绝，避免 `codeup:...` 被误当成工蜂项目路径。

### 4.2 数据模型改动

**`domain.Repository`**（JSON payload，无表结构变更）：

```go
Provider          string // github | gongfeng | ssh | yunxiao
GongfengProjectID string // legacy wire field; yunxiao -> "codeup:{org}/{path}"
YunxiaoRepoID     int    // Codeup numeric repository id, resolved lazily
```

**`domain.RepositoryContext`**：不新增秘密字段。Runner 只需 `CloneURL`、`Provider`、`GongfengProjectID`；Codeup 的 SSH 判定由 `CloneURL` 与 provider 共同决定。

**`domain.Project`**：

```go
YunxiaoSpaceID       string            `json:"yunxiaoSpaceId,omitempty"`
YunxiaoWorkItemTypes map[string]string `json:"yunxiaoWorkItemTypes,omitempty"`
YunxiaoStatusMap     map[string]string `json:"yunxiaoStatusMap,omitempty"`
YunxiaoDefaultAssigneeID string        `json:"yunxiaoDefaultAssigneeId,omitempty"`
```

**`domain.SyncConfig`**：

```go
YunxiaoAPIBase          string `json:"yunxiaoApiBase,omitempty"`
YunxiaoOrganizationID   string `json:"yunxiaoOrganizationId,omitempty"`
YunxiaoMCPURL           string `json:"yunxiaoMcpUrl,omitempty"` // 可选，官方托管 MCP
```

**`domain.User`**：

```go
YunxiaoToken            string `json:"-"`
YunxiaoTokenConfigured  bool   `json:"yunxiaoTokenConfigured,omitempty"`
YunxiaoCloneUser        string `json:"yunxiaoCloneUser,omitempty"` // P3 HTTPS 才使用
YunxiaoUserID           string `json:"yunxiaoUserId,omitempty"`    // 可选，工作项负责人匹配
```

**持久化**：

- `user_configs` 新增 `yunxiao_token`、`yunxiao_clone_user`、`yunxiao_user_id` 三列；`repo.Open` 的 AutoMigrate 会补列，另提供 `migrations/033_yunxiao_credentials.sql` 作为运维可手动执行的 DDL（不写数据、不写 seed）。
- 同步修改 `internal/repo/users.go` 的 `userSelectColumns`、`userWithConfig.toDomain`、`writeUserConfig` 与 `userHasConfig`；只在个人设置里填云效 PAT 的用户也必须创建/保留配置行。
- 其余字段都在 JSON payload 中，不需要数据迁移；旧记录没有新字段时按空值处理。

### 4.3 Provider seam

不进行一次性的“大抽象”，只把最容易扩散的 MR/托管差异收敛：

- 新增 `internal/tracker/hosting.go`（或同包 `provider.go`）：
  - `type hostingProvider interface { Name() string; FindMR(project, source, target string) (domain.DeliveryMR, bool, error); CreateMR(...); MergeMR(...); RefreshMR(...)}`
  - `providerForRemote(remote string) hostingProvider`：`github:` → GitHub；`codeup:` → 云效；其余 → 工蜂。
- `tracker.Service` 的 `FindBranchMR`、`EnsureAndMergeBranchMR`、`syncMRDeliveryStates` 改为先 `providerForRemote`，再调用适配器；现有 `githubFindBranch` / `githubMergeBranch` 保留为 GitHub 适配器实现。
- Issue 同步**暂不强行 interface 化**：`PullAll` / `PullScheduleJob` / `Create*` / `WriteBackClarification` 继续按 source 分支，新增 `yunxiao` case。等出现第三种非 MCP 工作项源时再抽象，避免大爆炸式重构。

### 4.4 `internal/yunxiao` 客户端

新增包，只负责 HTTP 与类型，不依赖 Gin / GORM：

```go
type Client struct { base, orgID, token string; http *http.Client; ... }
func New(base, orgID, token string) *Client
func (c *Client) GetRepository(ctx, repoPath string) (Repository, error)
func (c *Client) ListChangeRequests(ctx, repoID int, state string, page, perPage int) ([]ChangeRequest, error)
func (c *Client) GetChangeRequest(ctx, repoID, localID int) (ChangeRequest, error)
func (c *Client) CreateChangeRequest(ctx, repoID int, in CreateChangeRequestInput) (ChangeRequest, error)
func (c *Client) MergeChangeRequest(ctx, repoID, localID int, in MergeChangeRequestInput) (ChangeRequest, error)
func (c *Client) SearchWorkitems(ctx, in SearchWorkitemsInput) ([]Workitem, error)
func (c *Client) CreateWorkitem(ctx, in CreateWorkitemInput) (string, error)
func (c *Client) CreateWorkitemComment(ctx, workitemID, content string) (string, error)
func (c *Client) ListOrganizations(ctx) ([]Organization, error)
```

实现约束：

- 中心站路径带 `/organizations/{organizationId}`；Region 站不带。由 `base` 是否包含 `openapi-rdc.aliyuncs.com` 或显式 `region` 标志决定。
- 统一请求头：`x-yunxiao-token: <PAT>`、`Content-Type: application/json`。
- `http.Client` 超时 20–30 秒；`CheckRedirect` 禁止跨主机重定向，避免令牌泄漏。
- 响应体上限 8 MiB；错误体截断并脱敏；不记录请求头。
- 429/5xx 做有限重试（最多 2 次，带 jitter，尊重 `Retry-After`）；401/403 不重试，转成可操作提示。
- 分页读取 `x-total`、`x-total-pages`、`x-next-page`；Projex `page * perPage <= 10000` 是硬限制，超出时缩小时间窗而不是继续翻页。
- 缓存：仓库数字 ID、Codeup 用户 ID 映射、MR 状态（复用现有 2 分钟缓存思路）。
- 测试 seam：`tracker.Service` 增加可注入的 `yunxiaoAPIBase` / `yunxiaoClient`（与 `githubAPIBase` 一样），测试用 `httptest.Server`，不要依赖真实环境变量或真实云效。

### 4.5 Codeup 仓库与 MR

**仓库解析**

- 接受：
  - `https://codeup.aliyun.com/{org}/{path}`（含子组）
  - `https://codeup.aliyun.com/{org}/{path}.git`
  - `git@codeup.aliyun.com:{org}/{path}.git`
  - `ssh://git@codeup.aliyun.com[:22]/{org}/{path}.git`
  - `codeup:{org}/{path}`（显式前缀）
  - Region 站：`https://<org>.devops.aliyuncs.com/...`，由配置的 Web base 决定
- 拒绝：带账号密码的 URL、`..`、空段、非配置域名、把 Codeup 链接误判为通用 SSH。
- 解析结果：`Provider: "yunxiao"`、`GongfengProjectID: "codeup:{org}/{path}"`、`WebURL`、`CloneURL`（SSH 输入保留 SSH，否则 HTTPS）。
- `internal/httpapi/catalog.go` 的 `legacySSH` 增加 Codeup host，避免 `git@codeup...` 落到 `provider=ssh`。
- `web/app/git-ssh.js` 与 `chatbot/src/QuickAccessWizard.jsx` 的 SSH 预解析对 Codeup host 返回 `null`，交给 Codeup parser；否则前端会先识别成通用 SSH。

**仓库反查与客户端**

- `tracker` 增加 `yunxiaoClientForRemoteProject(remote string)`：遍历 `store.Repositories()`，按 `GongfengProjectID == remote` 找到仓库与所属项目，再取项目 Owner 的 PAT 构造客户端。
- 该函数是 `FindBranchMR`、`EnsureAndMergeBranchMR`、`syncMRDeliveryStates` 与对话提交 MR 的共同入口；`MRProject` 只有 `codeup:{org}/{path}`，数字仓库 ID 必须从仓库记录或 `GetRepository` 解析，不能从字符串硬猜。
- 找不到仓库/项目 Owner/PAT 时返回明确错误，不静默降级为工蜂客户端。

**仓库校验**

- 新增 `POST /api/repositories/:id/yunxiao/verify`（或复用 `PUT /api/repositories` 的 `verify=true`）：
  - 用当前用户 PAT 调 `GetRepository`（`repositoryId` 用 URL-encoded 完整路径或数字 ID）；
  - 校验 `provider=yunxiao`、路径与本地身份一致；
  - 回填 `YunxiaoRepoID`、`DefaultBranch`（用户未填时）、`WebURL`、`CloneURL`；
  - 返回权限级别 `accessLevel` 与是否可推送。
- 保存仓库本身不强制联网；未校验的仓库允许保存，但 UI 显示“待校验”，交付前必须校验成功。

**MR 查找**

- 用 `ListChangeRequests(projectIds=YunxiaoRepoID, state=opened, perPage=100)`，在客户端按 `sourceBranch`（大小写按 Git 规则）与 `targetBranch` 过滤。
- 找不到 opened 时，再查 `state=merged` 最近一页，避免重复创建；已关闭 MR 不作为可复用 MR。
- 最多翻 10 页；超过时记录日志并返回“未找到”，不无限翻页。
- `CreateChangeRequest` 入参：
  - `sourceBranch`、`targetBranch`
  - `sourceProjectId = targetProjectId = YunxiaoRepoID`
  - `title`、`description`（标题 ≤256 字符、描述 ≤10000 字符，超出时截断并保留完整内容到 artifact）
  - `workItemIds`：如果 issue 有云效 `ExternalRef.ExternalID`，一并关联
  - `reviewerUserIds`：MVP 不传；后续由项目配置
- `MergeChangeRequest`：
  - `mergeType` 默认 `no-fast-forward`（保留合并提交，行为接近现有工蜂 merge）；可配置为 `squash`/`ff-only`/`rebase`。
  - `removeSourceBranch` 默认 `false`，保留源分支，避免与现有“工作分支可复用”语义冲突。
  - 合并前检查 `allRequirementsPass`/`conflictCheckStatus`；失败时把云效原始信息放进错误，提示“卡点未通过/有冲突”，不吞掉。
- MR URL：优先使用响应 `webUrl`；缺失时按 `https://{webBase}/{org}/{path}/change/{localId}` 合成。
- 状态映射：
  - `UNDER_DEV` / `UNDER_REVIEW` / `TO_BE_MERGED` → `opened`
  - `MERGED` → `merged`
  - `CLOSED` → `closed`
- `mergedAt`：OpenAPI 的 `GetChangeRequest` 没有稳定的 `mergedAt` 字段；实现用状态为 `MERGED` 时的远端 `updateTime` 作为近似值，保证交付分析不会因零时间跳过云效 MR。该时间不是严格的合并时刻；P3 再通过 `mergedRevision` 查提交时间。

### 4.6 Projex 工作项

**拉取池**

- 以项目为单位，而不是仓库：项目配置了 `YunxiaoSpaceID` 才参与拉取。
- MVP 不支持按迭代过滤（`SearchWorkitems` 请求没有稳定的 `sprintId` 字段）；项目配置只到空间级。需要按迭代收窄时，后续通过 `conditions` 增加。
- 拉取类别：默认 `Req,Bug`，可通过项目配置扩展 `Task`。
- `SearchWorkitems` 请求：
  - `spaceId` = 项目 `YunxiaoSpaceID`
  - `category` = 配置的类别（逗号分隔）
  - `conditions` = 可选：按状态/更新时间过滤，避免 10000 条窗口
  - `page` / `perPage`（200）
  - `orderBy=gmtCreate` 或 `gmtModified`，`sort=desc`
- 分页：读到 `x-total-pages` 或不足一页为止；超过 10000 条时按 `gmtModified` 时间窗拆分。

**字段映射**

| Projex 字段 | 本地字段 | 说明 |
| --- | --- | --- |
| `id` | `ExternalRef.ExternalID` | 字符串 ID |
| `serialNumber` | `ExternalRef` 可选展示 / `Number` 为 0 | 不用 `ExternalIID` 存字符串 |
| `subject` | `Title` | |
| `description` | `Description` | 保留 `formatType=RICHTEXT/MARKDOWN` |
| `status.id/name/nameEn` | `ExternalRef.ExternalState` + `Status` | `Status` 走 `YunxiaoStatusMap`，再回退到 `nameEn` 关键词 |
| `assignedTo.id/name` | `AssigneeUserID` | 按平台登录名/中文名匹配；匹配不到保留空，手动导入用当前用户，自动同步用项目 Owner |
| `priority` 自定义字段 | `Priority` | 读取 `customFieldValues` 中 `priority` 的 `displayValue`；无法识别时 `medium` |
| `workitemType.name/id` | `ExternalRef.ExternalType` | `Req`/`Bug`/`Task` |
| `gmtCreate/gmtModified` | `CreatedAt/UpdatedAt` | 解析 ISO 8601 |
| 无 URL 字段 | `ExternalURL` | 用 `AGENTFLOW_YUNXIAO_WEB_BASE` + 空间/类别/ID 模板生成；上线前用真实工作项核对模板 |

**状态默认映射**

```text
TODO / 待处理 / 新建          -> todo
IN_PROGRESS / DOING / 处理中  -> in_progress
IN_REVIEW / 待评审 / 已解决    -> in_review
DONE / 已完成 / 已解决         -> done
CLOSED / 已关闭               -> closed
REJECTED / 已拒绝 / 取消       -> cancelled
其他                          -> backlog
```

`YunxiaoStatusMap` 优先于默认映射；工作流自定义时由项目维护。`ExternalState` 保留原始状态，`TrackerClosed` 增加对 `yunxiao` 的 `closed`/`done`/`cancelled` 判断，避免外部已关闭的工作项继续被派发。

**保留本地状态**

- 复用 `applyExistingIssue`：外部拉取只刷新 `Title/Description/ExternalRef`，不覆盖 `Status/CloseSource/Assignee/Workflow/DeliveryMR/RepositoryBranches`。
- 如果本地描述已被澄清修改（`SourceContentHash` 不一致），不要用远端描述覆盖；沿用 GitHub 的处理方式。
- 外部身份去重：`externalIssueKey` 增加 `yunxiao:{spaceID}:{externalID}`，避免同一工作项因多次导入产生重复。

**本地稳定 ID**

- 首次拉取生成 `yx-{projectID}-{externalID}`（工作项 ID 字符串可能包含特殊字符时做安全编码/截断）；同一项目内稳定，重复拉取沿用已有记录。
- `findYunxiaoIssue` 先按稳定 ID 查，再按 `Source=yunxiao + ExternalID` 查；不使用 `ExternalIID`（Projex ID 不是整数）。
- 从本地需求创建云效工作项后，若原 ID 是临时 `issue-*`，用稳定 ID 替换，并沿用现有工蜂创建的“删除旧临时记录”策略，但只删除确认属于该临时 ID 的记录。

**创建与写回**

- `CreateYunxiaoWorkitem(store, issue)`：
  - `CreateWorkitem` 只返回 ID；随后用 `GetWorkitem` 补齐标题/状态/编号/URL。`GetWorkitem` 的精确路径在实现时按官方文档核对；如果该版本不可用，退化为 `SearchWorkitems` 的 `id` 条件查询。
  - 必须能确定 `spaceId`、`workItemTypeId`、`assignedTo`。
  - `assignedTo` 解析顺序：issue 责任人 → 项目 `YunxiaoDefaultAssigneeID` → 项目 Owner 的云效用户 ID；都为空时返回可操作错误，不静默创建无负责人工作项。
  - `workItemTypeId` 按本地 `Kind` 与来源类别选择：需求 `Req`、缺陷 `Bug`、任务 `Task`。
  - 标题/描述直接映射；`formatType` 用 `MARKDOWN`（便于保留 LoopForge 生成的 Markdown）。
  - 创建返回 ID 后，用 `GetWorkitem` 补齐 `serialNumber/status/URL`，再写回 `ExternalRef`。
- `WriteBackClarification` 增加 `yunxiao` case：`POST /oapi/v1/projex/.../workitems/{id}/comments`，`formatType=MARKDOWN`。
- `POST /api/sync/create` 的 `target` 增加 `yunxiao`；`POST /api/issues` 的 `syncTarget` 增加 `yunxiao`；错误文案同步更新。

**定时任务**

- `ScheduleJob.Source` 增加 `yunxiao`；`NormalizeScheduleSource` 接受它。
- `PullScheduleJob` 增加 case：遍历配置了 `YunxiaoSpaceID` 的项目，调用 `pullYunxiaoWorkitems`。注意：当前生产路径并不调用 `PullScheduleJob`，真实拉取由 `PullAll`（自动同步/手动同步）完成；定时任务的 `Source=yunxiao` 只负责过滤已同步工作项。若未来要让每个定时任务执行前拉取，必须在 `internal/schedule` 显式接入并单列回归。
- `web` 与 `chatbot` 的来源单选/标签增加“云效”。

### 4.7 Git 传输与 SSH

- `domain` 增加 helper：
  - `RepositoryUsesSSH(provider, cloneURL string) bool`：`provider == ssh` 或 `provider == yunxiao` 且 `cloneURL` 是 SSH 引用。
  - `IsYunxiaoRemote(provider, remote string) bool`。
- `internal/engine`：
  - `sshRepositoryURLs` 使用 `RepositoryUsesSSH`，把 Codeup SSH 仓库也交给 `GitSSHKeysForRemotes`。
  - `withRepositoryGit`、`conversation_push`、`runner_align` 的 SSH 判断改用 helper。
  - `clarify_workspace.go:50` 直接判断 `repo.Provider == RepositoryProviderSSH`；改为 helper，否则 Codeup SSH 仓库在需求澄清阶段会被要求配置 Git Token。
  - `runner_dispatch.bindRunnerRepositories` 的 `UseSSH` 改用 helper；SSH 仓库清空 `GitToken`。
- `internal/runner`：`RunnerRepository.UseSSH` 已存在；`checkoutRepository` 与 `repositoryGitContext` 无需新增分支。Codeup SSH 的 host key 由现有 `AGENTFLOW_GIT_SSH_KNOWN_HOSTS` 管理。
- 错误提示：`gitTokenFixHint` 目前只写 GitHub；改为按 provider 返回提示。Codeup SSH 缺失 key 时显示“到个人配置添加 codeup.aliyun.com 的 SSH 私钥”；API 缺 PAT 时显示“配置云效访问令牌”。
- **P3 HTTPS**：如果用户坚持 HTTPS，需要新增 `YunxiaoCloneUser` 与“按仓库多凭据”机制；本方案不把它塞进 MVP。

### 4.8 同步编排

`tracker.Service.PullAll` 增加一段：

```go
for _, project := range store.Projects() {
    if strings.TrimSpace(project.YunxiaoSpaceID) == "" {
        continue
    }
    n, err := s.pullYunxiaoWorkitems(store, project)
    ...
}
```

与 TAPD 一样，云效按项目池拉取，不依赖仓库。日志 `SyncLog.Source = "yunxiao"`，`Action = "sync_issues"`，失败信息包含可操作的认证/权限提示。

`PullScheduleJob` 的 `yunxiao` case 只拉取指定项目（`job.ProjectID`）的空间；如果项目未配置 `YunxiaoSpaceID`，返回明确错误。该函数目前仅测试调用，生产拉取仍由 `PullAll` 的云效项目池完成；不要把定时任务验收建立在这条死代码路径上。

### 4.9 交付状态与分析

- `domain.MRURL` 统一为：

```go
func MRURL(provider, project string, iid int) string
```

`GongfengMRURL` 保留为兼容 wrapper。`codeup:` → `/{org}/{path}/change/{iid}`；`github:` → `/pull/{iid}`；其余 → 工蜂路径。

- `domain.ParseMRs` 统一解析 GitHub PR、工蜂 MR、Codeup `/change/`；`ParseGongfengMRs` 保留为 wrapper。`ParseGongfengMR` 的调用方逐步改为 `ParseMRs`/`ParseMR`，避免继续扩散 provider 判断。
- `internal/repo/delivery.go`：
  - artifact 扫描 SQL 增加 `OR run_artifacts.content LIKE '%/change/%'`（保留原有 `%merge_requests/%`）。
  - `analytics_business_merge.go`、`analytics_mr_merges.go` 的同类条件同步。
  - `branchContextForMRProject` 按 `GongfengProjectID` 匹配，`codeup:` 身份可直接命中。
  - `lookupMRDeliveryState` 使用统一 `ParseMR`。
- `MRDeliveryState.MRProject` 对新数据写 `codeup:{org}/{path}`；旧数据不变。
- Codeup 仓库路径按 API 返回的 `pathWithNamespace` 规范化；现有 `gongfengMRKey` 的小写化只用于工蜂/GitHub 兼容键，云效键是否保留大小写需在实现时确认（如果 Codeup 路径区分大小写，不能直接复用无条件小写化）。
- `internal/engine/prompt.go:159-173` 的 `hostedMRBlurb` 用 `strings.Contains(art.Content, "merge_requests")` 判断已有 MR；Codeup 的 `/change/` 不会命中，需同时识别 `/change/`，或改用统一 `ParseMRs` 判断。
- `mergedAt` 留零时，`MRDeliveryState` 的 `MergedAt` 为空；分类仍按 `State` 与 `TargetBranch` 工作。文档中明确“云效 MVP 不提供合并时间”。

### 4.10 Agent / MCP 集成

- 核心同步不使用 MCP；Agent 仍可通过 `workbench_workspace_submit_mr` 直接创建云效 MR。
- P3 可选：在 `internal/httpapi/mcp_market_catalog.json` 增加云效官方托管 MCP 条目：
  - URL：`https://openapi-rdc.aliyuncs.com/ai/mcp?toolsets=code-management,project-management`
  - 认证：`Authorization: Bearer <PAT>`（或 `X-Yunxiao-Token`）
  - Region：`https://<org>.devops.aliyuncs.com/ai/mcp?toolsets=...`
- 安装市场条目时目前只复制 `AuthType`，不会自动写 Headers；P3 需要补一个“安装后填写 Header”的向导，或在安装时让用户粘贴 PAT。MVP 不把 MCP 作为依赖，避免这个 UI 工作阻塞核心交付。
- `internal/httpapi/agent_tool_catalog.go` 的 `agentToolKindAllowlistNames` 目前只列 `tapd`/`gongfeng`；如果后续允许 Agent 使用云效 MCP 的只读工具，再增加 `yunxiao` 白名单。MVP 不开放未配置的 MCP 工具。

### 4.11 UI 改动

两个前端都要改，避免“Chatbot 能用、旧 Web 不能看”：

| 页面 | 改动 |
| --- | --- |
| 个人设置 | 新增“云效访问令牌”条目：粘贴 PAT、校验、显示组织/权限；P3 增加 HTTPS 克隆账号 |
| 项目设置 → 代码仓库 | 仓库地址支持 Codeup URL；显示 provider “云效 Codeup”；增加 `YunxiaoSpaceID`、工作项类型、状态映射；提供“校验云效仓库”按钮 |
| 同步配置 | 增加 `YunxiaoAPIBase`、`YunxiaoOrganizationID`、是否启用；显示“未配置”时不发请求 |
| 需求列表 | 来源标签/筛选增加“云效”；外部关联提示区分“云效工作项” |
| 触发配置 | 来源单选增加“只同步云效” |
| 交付/洞察 | 来源筛选、MR 链接、状态标签支持 `codeup:` URL |
| 错误提示 | PAT 失效、权限不足、仓库未校验、卡点未通过分别给出可操作文案 |

具体文件：

- `web/app/git-ssh.js`：`LoopForgeGitSSH` 目前只把 `github.com:22` 留给托管 API；Codeup host 也要返回 `null`，否则会被前端先识别成通用 SSH。
- `web/app/issues.js`：`parseGongfengRepoURL`、`repoProviderLabel`、`sourceLabel`/`tone`、保存仓库的 payload。
- `web/app/repo_drawer.js`、`web/app/projects.js`、`web/app/project-wizard.js`：仓库展示/表单。
- `web/app/workbench.js`：个人凭据条目、校验按钮。
- `web/app/sync-schedule.js`、`web/app/observability.js`：来源筛选与标签。
- `web/i18n.js` / `web/i18n-en.js`：新增文案（如果走翻译层）。
- `chatbot/src/QuickAccessWizard.jsx`：`parseGongfengRepo`、`parseGitHubRepo` 的 Codeup 分支、仓库表单、凭据提示。
- `chatbot/src/changes.js`：`branchCompareUrl` 目前只生成 GitHub/工蜂分支对比链接；增加 Codeup 仓库页/分支页链接，或对未知 host 返回空。
- `chatbot/src/credentials.js`：`gitToken` 之外的云效令牌提示。
- `web/app/issue-drawer.js`：`gongfengRefPath` 附近的分支 tree/compare 链接同样只认工蜂；增加 Codeup 或对未知 host 返回空。

### 4.12 安全

1. **最小权限**：PAT 只授予需要的 API 权限；文档写清“只读同步”和“需要合并 MR”两种情况。
2. **秘密边界**：PAT 只存在 `user_configs`，不进入 `Repository`、`Run`、`RunnerInvocation`、`RunCredentials` 的序列化；不写入日志、错误、artifact。
3. **脱敏**：`redactAssistantSecrets`、`redactAgentToolResult`、`gitwork.Redact` 增加 `YunxiaoToken`；`internal/httpapi/assistant_diagnostics.go:413` 的 secrets 列表也要加入云效 PAT；MCP headers 的 `Authorization` 继续由现有脱敏覆盖。
4. **SSRF/令牌外泄**：`YunxiaoAPIBase` 只接受 `https`，host 必须等于 `openapi-rdc.aliyuncs.com` 或以 `.devops.aliyuncs.com` 结尾；HTTP 客户端禁止跨主机重定向。
5. **权限校验**：所有云效写操作都以“当前用户/责任人/项目 Owner 的 PAT”执行，不能借用平台级共享令牌越权。
6. **审计**：同步日志记录 source、action、project、status、message；不记录请求头与响应体中的令牌。
7. **外部关闭**：`TrackerClosed` 覆盖云效终态，避免外部关闭后仍被定时任务派发。

### 4.13 兼容与迁移

- 旧仓库 `provider=gongfeng|github|ssh` 的行为不变；`GongfengProjectID` 的旧值没有 `codeup:` 前缀，不会进入云效分支。
- `MRDeliveryState` 旧行不变；新行用 `codeup:` 前缀。
- 新字段都是可选 JSON 字段或新列；旧客户端/旧数据读取时按空值处理。
- 不写启动 seed、不做数据回填、不在 `repo.Open` 里做云效初始化；只保留 AutoMigrate 的增量补列。
- 上线时通过 `AGENTFLOW_YUNXIAO_ENABLED`（或 `SyncConfig.YunxiaoAPIBase` 非空）控制入口；未配置时 UI 不显示云效入口，API 返回明确的“未启用”错误。

---

## 5. 实施阶段

### P0：基础 seam 与凭据（可独立合并）

**目标**：不改变现有行为，先让“云效”有位置可放。

- [ ] `domain`：增加 `yunxiao` provider/source 常量、`RepositoryUsesSSH`、`MRURL`/`ParseMRs` 的统一入口（旧函数保留 wrapper）。
- [ ] `domain`：`GongfengRepoInfo` 增加 `Provider` 字段；`ParseRepositoryURL` 增加 Codeup 解析与测试。
- [ ] `internal/yunxiao`：新建客户端骨架（配置、请求、错误、分页、脱敏），先只实现 `GetRepository` 与 `ListOrganizations`。
- [ ] `repo`/`domain.User`：新增云效 PAT 字段、`UserCredentialUpdate`、`PublicUser` 脱敏、`user_configs` 列与 `033` DDL。
- [ ] `httpapi`：新增 `POST /api/users/:id/yunxiao-token/verify`；`/api/capabilities` 增加云效可用性；`redactAssistantSecrets` 增加 PAT。
- [ ] 测试：URL 解析、MR URL 解析、凭据脱敏、旧 provider 回归。

**验收**：现有 GitHub/工蜂/TAPD/SSH 测试全绿；云效字段可保存、可脱敏、可校验；未配置时行为不变。

### P1：Codeup 仓库 + MR 交付

**目标**：绑定 Codeup 仓库，对话与 SOP 都能创建/合并 MR，交付分析能看到状态。

- [ ] `httpapi/catalog.go`：`legacySSH` 识别 Codeup；provider 校验增加 `yunxiao`；新增 `POST /api/repositories/:id/yunxiao/verify`。
- [ ] `domain`/`httpapi`：Codeup 仓库身份写入 `codeup:{org}/{path}`；回填 `YunxiaoRepoID`、`DefaultBranch`、`WebURL`、`CloneURL`。
- [ ] `internal/yunxiao`：实现 `ListChangeRequests`、`GetChangeRequest`、`CreateChangeRequest`、`MergeChangeRequest`。
- [ ] `tracker`：新增云效 hosting adapter；`FindBranchMR`、`EnsureAndMergeBranchMR`、`syncMRDeliveryStates` 分流。
- [ ] `domain`/`repo`：`MRURL`、`ParseMRs`、artifact SQL、`branchContextForMRProject` 支持 `codeup:`。
- [ ] `engine`：`RepositoryUsesSSH` 接入 `sshRepositoryURLs`、`withRepositoryGit`、`bindRunnerRepositories`；Codeup SSH 仓库不需要 GitToken。
- [ ] `httpapi/assistant_workspace_mr.go`：对话提交 MR 增加云效分支，使用请求人的 PAT；返回 `mrUrl` 与 `warning`。**同时拆开现有 `apiToken == ""` 前置检查**：Codeup SSH 仓库不需要 GitHub GitToken，但需要 `YunxiaoToken`；不能因为用户没配 GitHub Token 就在进入云效分支前拒绝。
- [ ] `web`/`chatbot`：仓库表单、provider 标签、凭据提示、校验按钮。
- [ ] 测试：fake Yunxiao 服务覆盖创建/复用/合并/状态；codeup artifact 发现；Runner `UseSSH`；旧流程回归。

**验收**：

- 绑定 `git@codeup.aliyun.com:org/repo.git` 后，能检出并推送工作分支。
- 对话提交能创建/复用 MR；SOP 交付能合入目标分支；`deliveryMrs` 与 `deliveryMrState` 正确。
- MR 状态刷新能识别 `MERGED`/`CLOSED`/`opened`；交付分析能识别 `/change/` 链接。
- 未配置 PAT/SSH key 时错误可操作。

### P2：Projex 工作项同步

**目标**：云效工作项进入需求列表，支持创建、评论、定时触发。

- [ ] `domain.Project`：`YunxiaoSpaceID`、`YunxiaoWorkItemTypes`、`YunxiaoStatusMap`、`YunxiaoDefaultAssigneeID`。
- [ ] `internal/yunxiao`：`SearchWorkitems`、`GetWorkitem`、`CreateWorkitem`、`CreateWorkitemComment`。
- [ ] `tracker`：`pullYunxiaoWorkitems`、`CreateYunxiaoWorkitem`、`WriteBackClarification` 的云效 case、`PullAll`/`PullScheduleJob` 分流。
- [ ] `domain`：`NormalizeScheduleSource` 接受 `yunxiao`；`TrackerClosed` 覆盖云效终态；`externalIssueKey` 增加云效键。
- [ ] `httpapi`：`/api/sync/create`、`/api/issues` 的 `syncTarget` 增加 `yunxiao`；错误文案更新。
- [ ] `repo`：`DefaultWorkflowID` / `isRequirement` 识别云效 `Bug` 与 `Req`；`knownWorkflow` 同步。
- [ ] `web`/`chatbot`：来源标签/筛选、触发配置来源、工作项配置表单。
- [ ] 测试：工作项映射、状态映射、保留本地状态、评论写回、创建时负责人解析、定时任务池。

**验收**：

- 项目配置空间后，需求/缺陷能拉取；重复拉取不覆盖本地状态；外部关闭阻止派发。
- 本地需求能创建云效工作项；澄清摘要能写回评论。
- 触发配置选择“云效”后只拉云效工作项并派发。

### P3：可选增强

- [ ] Flow 流水线：仓库上的 pipeline URL 已有展示位；再评估 `create_pipeline_run` / `get_latest_pipeline_run` 与 MR 卡点。
- [ ] Codeup HTTPS：`YunxiaoCloneUser` + 多主机 Git 凭据重写；需要改 `gitwork.SandboxEnv`、`RunCredentials`、Runner 协议。
- [ ] 云效 OAuth：复用官方 MCP 的 OAuth 或 OpenAPI OAuth；需要回调地址与令牌刷新。
- [ ] 官方 MCP 市场条目 + 安装后 Header 向导。
- [ ] Webhook 驱动同步：用 Codeup Webhook 替代部分轮询；需要公网回调、签名校验、幂等与重放保护。
- [ ] MR 合并时间：用 `mergedRevision` 查提交时间，补齐 `MergedAt`。

---

## 6. 测试计划

### 6.1 单元测试

- `internal/domain`：Codeup URL 解析（web/ssh/scp/裸路径/Region/凭据/路径穿越）；`codeup:` MR URL 解析与生成；旧 GitHub/工蜂解析不回归。
- `internal/yunxiao`：请求路径（中心站/Region）、`x-yunxiao-token`、分页头、401/403/429/5xx、响应截断、令牌脱敏。
- `internal/tracker`：Codeup 状态映射、MR 查找复用、创建/合并入参、工作项字段映射、状态映射、`applyExistingIssue` 保留本地状态、`externalIssueKey` 去重。
- `internal/repo`：artifact SQL 能发现 `/change/` URL；`branchContextForMRProject` 对 `codeup:` 生效；旧 `merge_requests` 行为不变。
- `internal/httpapi`：仓库登记/校验、`syncTarget=yunxiao`、对话提交 MR、令牌校验、错误文案。
- `internal/engine`：`RepositoryUsesSSH`、`sshRepositoryURLs`、`bindRunnerRepositories`、Runner `UseSSH`。

### 6.2 集成测试

- 用 `httptest.Server` 模拟云效 OpenAPI：
  - 仓库校验 → 返回数字 ID/defaultBranch/URL；
  - 创建 MR → 返回 `localId/webUrl/status`；
  - 列表 MR → 能按 source/target 过滤；
  - 合并 MR → 返回 `MERGED`；
  - 工作项搜索 → 分页、状态、负责人；
  - 创建工作项/评论 → 返回 ID。
- `e2e/api`：仓库登记与 sync 端点；不实际访问云效。
- `webtest`：仓库表单识别 Codeup、来源标签、凭据提示。

### 6.3 手工/真实环境验收（不进 CI）

- 真实云效组织、真实 Codeup 仓库、真实 Projex 空间。
- 验证：SSH 克隆/推送；PAT 校验；创建 MR；合并 MR；状态刷新；工作项拉取/创建/评论；定时任务派发。
- 记录云效 API 版本/域名/权限点，作为部署文档的一部分。

---

## 7. 风险与缓解

| 风险 | 影响 | 缓解 |
| --- | --- | --- |
| OpenAPI 路径/字段随版本变化 | 同步失败 | 固定 `/oapi/v1`，客户端集中在 `internal/yunxiao`；用 fake server 覆盖契约；错误信息保留原始状态 |
| 中心站/Region 路径差异 | Region 用户不可用 | 用 `YunxiaoAPIBase` 判定是否带组织路径；两条路径都写测试 |
| 仓库数字 ID 未解析 | 创建 MR 失败 | 保存时允许待校验；`verify` 端点回填；首次使用时再解析并缓存 |
| MR 列表没有 sourceBranch 过滤 | 查找慢/漏 | 按项目 `state=opened` 分页 + 客户端过滤；限制 10 页；找不到再查 merged 最近一页 |
| Projex 工作流自定义 | 状态映射错误 | `YunxiaoStatusMap` 优先；默认仅按 `nameEn` 关键词；UI 可配置 |
| 工作项负责人匹配不到 | 创建失败 | 项目级默认负责人兜底；仍为空时给出明确错误，不静默创建 |
| 分页 10000 条上限 | 漏数据 | 按 `gmtModified` 时间窗拆分；日志记录截断 |
| 没有 mergedAt | 交付时间不准 | MVP 不伪造；分类按状态；P3 用 `mergedRevision` 补 |
| 混合 GitHub/Codeup 的 Git 凭据 | HTTPS token 串用 | MVP 用 Codeup SSH；P3 再做多主机凭据 |
| UI 两套重复实现 | 行为不一致 | 同一套解析规则写测试；`web` 与 `chatbot` 同步改 |
| 公开版默认无云效配置 | 用户困惑 | 未配置时隐藏入口/返回“未启用”；文档说明配置步骤 |
| 令牌泄漏 | 安全事故 | 只存用户配置；日志/错误/URL 脱敏；禁止跨主机重定向；PAT 最小权限 |

**回滚**：关闭 `AGENTFLOW_YUNXIAO_ENABLED` 或清空 `YunxiaoAPIBase`，UI 入口消失；旧 provider 不受影响；新增列/字段可保留，不做破坏性删除。

---

## 8. 验收清单

- [ ] 绑定 Codeup 仓库（SSH）后，`/api/repositories` 返回 `provider=yunxiao`、`codeup:{org}/{path}`、数字仓库 ID。
- [ ] 对话提交 MR 返回云效 `mrUrl`；重复提交复用已有 opened MR，不重复创建。
- [ ] SOP 交付能创建并合并 MR；`deliveryMrs` 每仓库一条；`deliveryMrState` 聚合正确。
- [ ] MR 状态刷新能处理 `UNDER_*`/`MERGED`/`CLOSED`；`/change/` artifact 能被发现。
- [ ] Projex 工作项拉取、创建、评论、定时触发可用；本地状态/澄清/MR 绑定不被覆盖。
- [ ] 外部关闭的工作项不会继续被派发。
- [ ] 令牌校验、权限不足、仓库未校验、SSH key 缺失都有可操作提示。
- [ ] 日志、UI、API 响应中不出现 PAT。
- [ ] 现有 GitHub/工蜂/TAPD/SSH 测试全部通过。
- [ ] `docs/api`、`openapi.yaml`、部署文档与 README 的 provider/source 说明同步更新。

---

## 9. 待确认问题

1. **Codeup 仓库的 Web/克隆 URL 规范**：中心站按 `https://codeup.aliyun.com/{org}/{path}` 设计；Region 站的实际 Web 域名是否总是 `<org>.devops.aliyuncs.com`，需要在真实 Region 环境核对后再固定默认值。
2. **Projex 工作项 URL 模板**：官方搜索响应没有 `webUrl`，本方案用可配置模板生成；上线前需用真实需求/缺陷各核对一次。
3. **工作项负责人匹配**：云效用户 ID 与平台登录名的稳定映射关系需要真实组织样本；如果多数组织无法按登录名匹配，P2 需要增加“用户级云效 ID”配置或成员同步。
4. **合并策略**：默认 `no-fast-forward` 是否符合团队保护分支策略；如果仓库只允许 `squash`，需要在项目级可配置。
5. **PAT 权限点名称**：不同云效版本的权限点命名可能不同；部署文档应写“按控制台实际名称勾选”，不硬编码权限点 ID。

---

## 10. 参考

### 官方文档

- [云效 API 参考（标准版）](https://help.aliyun.com/zh/yunxiao/developer-reference/api-reference-standard-proprietary/)
- [服务接入点（中心站/Region 站）](https://help.aliyun.com/zh/yunxiao/developer-reference/service-access-point-domain)
- [获取个人访问令牌](https://help.aliyun.com/zh/yunxiao/developer-reference/obtain-personal-access-token)
- [CreateChangeRequest](https://help.aliyun.com/zh/yunxiao/developer-reference/createchangerequest-create-merge-request)
- [GetChangeRequest](https://help.aliyun.com/zh/yunxiao/developer-reference/getchangerequest-query-merge-request)
- [ListChangeRequests](https://help.aliyun.com/zh/yunxiao/developer-reference/listchangerequests-query-the-list-of-merge-requests)
- [MergeChangeRequest](https://help.aliyun.com/zh/yunxiao/developer-reference/mergechangerequest-merge-merge-request)
- [GetRepository](https://help.aliyun.com/zh/yunxiao/developer-reference/getrepository-query-the-code-base)
- [SearchWorkitems](https://help.aliyun.com/zh/yunxiao/developer-reference/searchworkitems)
- [CreateWorkitem](https://help.aliyun.com/zh/yunxiao/developer-reference/createworkitem)
- [CreateWorkitemComment](https://help.aliyun.com/zh/yunxiao/developer-reference/createworkitemcomment)
- [ListOrganizations](https://help.aliyun.com/zh/yunxiao/developer-reference/listorganizations)
- [云效 MCP Server 使用说明](https://help.aliyun.com/zh/yunxiao/developer-reference/cloud-effect-mcp-tool-instructions)
- [alibabacloud-devops-mcp-server](https://github.com/aliyun/alibabacloud-devops-mcp-server)
- [配置 HTTPS 密码（Codeup 克隆账号）](https://help.aliyun.com/zh/yunxiao/user-guide/configure-https-clone-account-password)

### 仓库内相关代码

- `internal/domain/project.go`
- `internal/domain/public_repository.go`
- `internal/domain/delivery_mr.go`
- `internal/domain/mr.go`
- `internal/domain/issue.go`
- `internal/domain/sync.go`
- `internal/tracker/service.go`
- `internal/tracker/merge_mr.go`
- `internal/tracker/mr_delivery_sync.go`
- `internal/tracker/github.go`
- `internal/repo/delivery.go`
- `internal/repo/catalog.go`
- `internal/repo/users.go`
- `internal/repo/models.go`
- `internal/httpapi/catalog.go`
- `internal/httpapi/assistant_workspace_mr.go`
- `internal/httpapi/sync.go`
- `internal/httpapi/issues.go`
- `internal/httpapi/schedule.go`
- `internal/engine/git_auth.go`
- `internal/engine/conversation_push.go`
- `internal/engine/runner_dispatch.go`
- `internal/engine/lifecycle.go`
- `internal/gitwork/auth.go`
- `internal/gitwork/ssh.go`
- `web/app/issues.js`
- `web/app/repo_drawer.js`
- `web/app/workbench.js`
- `web/app/sync-schedule.js`
- `web/app/observability.js`
- `chatbot/src/QuickAccessWizard.jsx`
- `chatbot/src/credentials.js`
- `docs/api/collaboration.md`
- `docs/api/sync.md`
- `docs/api/openapi.yaml`

---

## 11. 文件级改动清单（实施时按此展开）

### 后端 domain / repo

- `internal/domain/project.go`：`Repository.Provider` 注释、`YunxiaoRepoID`、`Project` 云效字段、`GongfengRepoInfo.Provider`、Codeup URL 解析。
- `internal/domain/public_repository.go`：`ParseRepositoryURL` 增加 Codeup/Region。
- `internal/domain/delivery_mr.go`：`MRURL`、`ParseMRs` wrapper、Codeup URL。
- `internal/domain/mr.go`：统一 MR 解析与去重；保留旧函数。
- `internal/domain/issue.go`：`TrackerClosed` 覆盖云效；`ExternalRef` 注释。
- `internal/domain/sync.go`：`SyncConfig` 云效字段、`NormalizeScheduleSource`、`ScheduleJob` 注释。
- `internal/domain/user.go`：云效 PAT 字段与 `PublicUser` 脱敏。
- `internal/repo/models.go`、`internal/repo/users.go`、`internal/repo/users_credentials.go`（如有）：新列、读写、`PublicUser`、`UserCredentialUpdate`。
- `internal/repo/delivery.go`、`internal/repo/analytics_business_merge.go`、`internal/repo/analytics_mr_merges.go`：artifact SQL 与 URL 解析。
- `internal/repo/catalog.go`：`DefaultWorkflowID` / `isRequirement` 云效类别。
- `migrations/033_yunxiao_credentials.sql`：可选 DDL。

### tracker / yunxiao

- `internal/yunxiao/client.go`、`types.go`、`errors.go`、`client_test.go`。
- `internal/tracker/provider.go`（hosting seam）。
- `internal/tracker/yunxiao.go`（MR/仓库适配）。
- `internal/tracker/yunxiao_workitems.go`（Projex 适配）。
- `internal/tracker/service.go`：Pull/Push/定时任务分支。
- `internal/tracker/mr_delivery_sync.go`：云效刷新分支。
- `internal/tracker/merge_mr.go`：云效合并分支。
- `internal/tracker/catalog.go`：可选云效 MCP 绑定（P3）。

### engine / runtime / runner

- `internal/engine/git_auth.go`：SSH helper、提示文案。
- `internal/engine/conversation_push.go`、`internal/engine/clarify_workspace.go`、`internal/engine/runner_align.go`、`internal/engine/runner_dispatch.go`：SSH 判定。
- `internal/engine/lifecycle.go`：无需改逻辑，依赖统一 MR 解析。
- `internal/gitwork/auth.go`：P3 多主机凭据；MVP 不改。
- `internal/runner/daemon.go`：SSH 路径复用；MVP 不改协议。

### httpapi

- `internal/httpapi/catalog.go`：provider/URL/校验。
- `internal/httpapi/assistant_workspace_mr.go`：云效 MR 创建。
- `internal/httpapi/sync.go`、`internal/httpapi/issues.go`、`internal/httpapi/schedule.go`：source/target 枚举与分支。
- `internal/httpapi/users.go`、`internal/httpapi/server.go`：云效令牌校验路由。
- `internal/httpapi/agent_tool_catalog.go`：脱敏。
- `internal/httpapi/mcp_market_catalog.json`：P3 云效 MCP 条目。

### 前端 / 文档 / 测试

- `web/app/issues.js`、`web/app/repo_drawer.js`、`web/app/projects.js`、`web/app/project-wizard.js`、`web/app/workbench.js`、`web/app/sync-schedule.js`、`web/app/observability.js`、`web/i18n.js`、`web/i18n-en.js`。
- `chatbot/src/QuickAccessWizard.jsx`、`chatbot/src/credentials.js`。
- `docs/api/collaboration.md`、`docs/api/sync.md`、`docs/api/openapi.yaml`、`docs/deployment.md`、`docs/architecture.md`、`docs/open-source-scope.md`。
- `e2e/api/*`、`webtest/tests/*`、`internal/**/*_test.go`。

---

## 12. 实施顺序建议（PR 粒度）

1. **PR 1（P0）**：domain 常量/URL/MR 解析 + `internal/yunxiao` 客户端骨架 + 云效 PAT 字段/脱敏/校验端点 + 测试。不动现有交付路径。
2. **PR 2（P1a）**：仓库登记/校验 + Codeup SSH 传输 + 前端仓库表单。
3. **PR 3（P1b）**：MR 查找/创建/合并 + 交付分析 SQL/URL + 对话提交 MR + 测试。
4. **PR 4（P2a）**：Projex 拉取/字段映射/保留本地状态 + 来源标签。
5. **PR 5（P2b）**：Projex 创建/评论/定时任务 + 默认 SOP 映射。
6. **PR 6（P3，可选）**：Flow、HTTPS、OAuth、MCP 市场、Webhook。

每个 PR 都应保持旧 provider 测试全绿；涉及共享解析函数的改动先加新用例，再改实现。

---

## 13. 评审记录

- 2026-11-16：作者完成代码事实核对与自检，补充了 `GongfengProjectID` 复用边界、Codeup SSH 与 `UseSSH`、`assistant_workspace_mr` 前置凭据检查、`userHasConfig`、artifact `/change/` 扫描、前端分支对比链接等遗漏。
- 独立 subagent 复核两次均被会话侧取消，未产出报告；本文状态仍为“设计建议，待评审”。
