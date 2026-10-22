# AGENTS.md

本文适用于整个仓库，供在 CornerStone 中工作的编码代理使用。优先依据实际源码、配置和测试判断当前行为；README 中的功能概览不一定覆盖最新实现。

## 项目概况

CornerStone 是自托管 AI 聊天应用，使用 Go 后端与 React + TypeScript 前端，不依赖外部数据库。

- 后端：Go `1.26.10`（见 `go.mod`），模块名 `cornerstone`，使用标准库 `net/http` 路由。
- 前端：React 19、TypeScript 7、Vite 8、React Router 7、Motion 14（`motion/react`）；使用 npm 与 `web/package-lock.json`。
- 聊天提供商：OpenAI Chat Completions、OpenAI Responses、Gemini、Anthropic；另有 Gemini 生图客户端和 MiniMax TTS。
- Gemini 聊天/记忆的 `gemini_automatic_continuation` 默认关闭，开启后 SDK 可发起多次计费请求；聊天仍用 GenerateContent。生图客户端用 Interactions API，`store=false`、单次单张、不自动重试；旧 Imagen 方法明确拒绝请求，当前没有业务生图入口。
- 功能包括人设、流式聊天、多模态消息、记忆提取与写入、工具调用、联网搜索、天气、时间、定时提醒和空闲主动问候。
- 通道包括 Web、ClawBot / 微信 iLink、NapCat / QQ（仅私聊，OneBot 反向 WebSocket）和 QQ 官方机器人（仅 C2C 私聊，官方 WebSocket，支持扫码绑定）；修改共享回复逻辑时需考虑各通道。
- ClawBot 有持久化 inbox / outbox、引用缓存和人工投递恢复；NapCat 支持私聊媒体接收、引用 / 合并转发展开及确认后写入投递历史，但不要假定它具有同样的持久化队列。
- 数据使用本地 JSON / JSONL 文件持久化；前端构建文件由 Go 服务从磁盘提供，未嵌入二进制。

## 代码导航

| 路径 | 职责 |
| --- | --- |
| `main.go` | CLI、数据路径、TLS、管理器和后台服务初始化、API 与 SPA 静态文件服务 |
| `api/handler.go`、`api/auth.go` | 路由注册、统一响应、请求体限制、鉴权与首次初始化限制 |
| `api/handler_chat.go`、`api/session_reply.go` | Web 聊天与共享会话回复逻辑 |
| `api/chat_tool_*.go`、`api/chat_tools_executor.go` | 工具循环、执行器、历史回放与各工具实现 |
| `api/clawbot*.go` | iLink 连接、扫码、typing、入站媒体与引用、持久化投递及历史对账 |
| `api/napcat*.go` | OneBot 反向 WebSocket、连接生命周期、私聊媒体与引用解析、确认投递 |
| `api/qqofficial*.go`、`client/qqofficial*.go`、`config/qqofficial.go` | QQ 官方私聊、扫码绑定、token / 网关生命周期、文本与图片投递；不处理群或频道 |
| `storage/qqofficial_receipts.go` | 独立于可删除会话的 QQ 官方入站去重回执（保留 30 天，上限 100000 条，满额或损坏时拒绝继续处理） |
| `api/chat_tool_attachment.go`、`api/handler_attachment.go` | `send_attachment` 投递意图与受鉴权保护的附件下载 |
| `api/reminder_service.go`、`api/idle_greeting_service.go` | 定时提醒与主动问候 |
| `client/` | 上游 AI、图片、TTS、天气、ClawBot 客户端及共享 HTTP 客户端 |
| `config/` | 配置结构、默认值、旧配置兼容、归一化、校验与持久化 |
| `storage/` | 会话、人设、用户、鉴权、记忆、提醒和问候状态持久化；记忆提取流程 |
| `storage/clawbot*.go`、`storage/attachments.go`、`storage/attachment_cleanup.go` | ClawBot 队列 / 引用快照、附件存储及启动前孤儿清理 |
| `internal/search/` | 联网搜索接口、注册表与编排；`providers/` 包含 Exa、Tavily、Querit、Zhipu、SearxNG |
| `exacttime/` | NTP 校时与可注入时间服务 |
| `logging/` | 应用日志 |
| `web/src/App.tsx`、`web/src/utils/routes.ts` | 页面切换与前端路由 |
| `web/src/components/ChatDetail/` | 聊天界面，拆分为 components、hooks、红包交互等模块 |
| `web/src/components/PersonaEditor/` | 人设与记忆编辑界面 |
| `web/src/components/ChannelsPage.tsx`、`ClawBotSettings.tsx`、`NapCatSettings.tsx`、`ClawBotDeliveryQueue.tsx`（同目录） | 通道配置、状态与 ClawBot 投递恢复 UI |
| `web/src/services/`、`web/src/types/` | API 调用封装与前端数据类型 |
| `web/src/contexts/`、`web/src/i18n/` | 主题、语言、Toast、确认框，以及中英文文案 |
| `web/src/utils/` | 时间、动效、通知、回复拆分与等候窗口等共享逻辑 |
| `web/vite.config.ts`、`web/public/sw.js` | 开发代理、构建插件、版本注入与 Service Worker |
| `.github/workflows/release.yml` | tag 触发的多平台构建、打包与校验和生成 |

## 开发与验证命令

从仓库根目录执行，除非命令显式切换目录。Go 版本以 `go.mod` 为准；发布工作流使用 Node.js 24 LTS，本地 Node 版本还需满足锁文件中 Vite 等依赖的 engines 要求。

```bash
# 安装前端依赖（有锁文件，优先 npm ci）
(cd web && npm ci)

# 后端开发：显式指定数据目录，避免 go run 使用临时可执行文件旁的默认路径
go run . -port 1205 -data ./src

# 另一个终端启动前端开发服务
(cd web && npm run dev)

# 后端测试、静态检查与构建
go test ./...
go vet ./...
go build -o cornerstone .

# 前端类型检查及生产构建；格式检查
(cd web && npm run build)
(cd web && npm run format:check)

# 使用生产构建启动
./cornerstone -port 1205 -data ./src -web ./web/dist
```

- Vite 开发服务默认使用 `http://localhost:3000`，将 `/api` 和 `/management` 代理到 `http://127.0.0.1:1205`。修改后端端口时需同步代理配置。
- Go 默认端口为 `1205`。不传 `-data` 时，数据目录为**可执行文件旁边**的 `src/`，不是固定的仓库目录。
- 前端静态目录可通过 `-web` 指定；默认依次查找可执行文件旁和当前工作目录下的 `web/dist`。没有构建前端时仍可启动 API。
- 首次账号初始化只允许 localhost 访问；开发时不要为了远程调试移除此限制。
- TLS 使用 `-tls-cert` / `-tls-key`，或配置中的 `tls_cert_path` / `tls_key_path`；证书和私钥必须成对提供。
- 聚焦 Go 测试可使用 `go test ./api -run TestName -count=1` 等包级命令；修改并发状态或后台服务时建议运行相关包的 `go test -race`。
- 外部通道 / 附件改动可先运行 `go test ./api ./client ./storage -run 'Test.*(ClawBot|NapCat|Attachment|Delivery)' -count=1`；再按影响补跑整包或全量测试，不以筛选测试代替完整回归。
- 联调应使用独立的 `-data` 测试目录；上面的 `./src` 示例会使用仓库运行时数据，不应拿来做破坏性验证，也不要同时启动多个实例共用该目录。
- 前端目前没有 `test` 或 `lint` 脚本，不要声称运行了这些检查。`npm run build` 包含 `tsc` 类型检查。
- `npm run format` 会重写所有匹配的 TS/TSX 文件；仅为局部改动时，避免顺手全仓格式化。

## 修改约定

### 后端与接口

- 延续现有分层：HTTP 处理放在 `api/`，上游协议适配放在 `client/`，持久化操作通过 `storage/` 管理器完成。
- Go 修改使用 `gofmt`，测试放在对应包的 `*_test.go` 中。优先沿用 `httptest`、假客户端和 `t.TempDir()`，不依赖真实密钥、付费上游或用户数据。
- 新管理接口通常在 `api/handler.go` 中通过 `registerProtectedRoute` 注册，使用现有 `decodeJSON` / `jsonResponse` 与 `{success, data?, error?}` 响应格式；流式聊天遵循其独立协议。
- 公开接口是明确例外：鉴权状态 / 初始化 / 登录、健康检查，以及使用独立 access token 的 NapCat WebSocket。不要把新接口默认暴露为公开路由。
- 后端鉴权 token 保存在内存中，重启失效；前端使用 localStorage 保存 token 并发送 Bearer header，后端也支持鉴权 Cookie。变更时同时检查 `api/auth.go` 与 `web/src/services/api.ts`。
- 修改 API 字段时同步 Go JSON tags、`web/src/types/`、相关 `web/src/services/`、设置 UI 和测试。保留已有兼容字段与别名，除非任务明确要求迁移。
- 新增配置项需覆盖默认值、加载旧配置时的补齐、归一化、校验、更新和保存路径；注意“字段缺失”与“显式 false / 0 / 空字符串”可能有不同含义。
- 保持管理器的锁和快照边界；不要在调用方直接修改共享 map、slice 或绕过管理器写文件。后台循环应遵循现有 context 取消与关闭约定。

### 聊天与工具链路

- 流式响应协议改动需同时检查 `api/handler_chat.go`、`web/src/services/api.ts` 与 `ChatDetail/hooks/`，包括事件载荷、`[DONE]`、中断、错误和落盘时机。
- 修改消息历史时保留 `tool_calls`、`tool_call_id`、推理内容、图片、TTS、`attachments`、引用及引用附件、回复拆分元数据；验证分页、编辑、撤回、重新生成和按轮次截取上下文。
- 外部投递修改历史须使用 `storage.ChatManager` 的幂等 / 条件替换接口，保留 `applied_operations` 回执；历史编辑或删除后，不应被迟到的旧投递覆盖或重新追加。
- 回复拆分使用 `assistant_message_split_token`：默认 `→`，显式空字符串关闭拆分。历史消息可以保存自己的 token，不要用当前全局设置覆盖历史值；前后端解析语义需一致。
- 不要重新引入纯空白 assistant 消息落盘；同时不要误删有工具调用、推理、多媒体等有效载荷的消息，或破坏 `no_reply` 的刻意静默行为。参考现有空消息过滤与测试。
- 主动回复需沿用现有内容归一化逻辑，避免把 `<think>` 内容发送给用户。
- 新工具需检查工具定义、执行器注册、工具开关、通道权限、历史回放、前端展示和对应测试，不能只增加一个执行函数。工具循环有步数上限，不应绕开。
- 新搜索提供商放在 `internal/search/providers/` 并注册到 `register.go`；沿用编排层的超时、结果归一化和域名排除逻辑。
- 涉及时间与调度时优先沿用 `exacttime` 服务和现有时区配置，测试中注入时间来源。

### 外部通道、投递与媒体

- 区分“生成成功”“生成投递意图”“上游发送确认”“历史提交成功”。`send_attachment` 成功仅生成意图，不读任意文件、不发网络请求，也不代表已送达；实际发送和去重由通道投递层处理。
- ClawBot 入站任务、去重回执、context 与游标保持原子提交；保留 outbox 分段状态、来源关联和恢复语义。发送结果不确定时等待人工处理，不自动盲重发；已确认发送但历史提交失败时只对账，不再发送。
- NapCat action 响应通过 echo 关联等待者；维持连接 / 账号校验、断连取消和旧连接隔离。发送超时不等于未送达，不自动重试；部分发送失败应停止后续段，历史只提交已确认部分。
- QQ 官方通道只消费 `C2C_MESSAGE_CREATE`，即使订阅位同时覆盖群事件，也必须丢弃群 / 频道事件。会话按 AppID、用户 OpenID、人设隔离；OpenID 不是 QQ 号。当前仅文本和图片，不启用提醒、主动问候或语音转写；`send_attachment` 仅允许当前会话登记的图片。
- QQ 官方扫码绑定密钥及 AppSecret 不得返回浏览器或写日志；过期、取消、配置变更、新扫码必须使旧结果失效。官方 REST 消息 POST 不盲重试；恢复网关时允许 `RESUMED` 之前的重放事件。投递前及分段间检查历史未被编辑 / 删除，确认后条件提交，并保留工具调用的 provider replay 状态。
- 调整提醒 / 主动问候启动顺序时，确保生产者领取任务前已挂载持久化通道；typing 是非阻塞的辅助状态，不应影响回复生成或投递可靠性。
- iLink 的 `context_token` 是不透明字符串，原样存储和回传，不裁剪空白；空值不清除已有值，重复消息不覆盖较新的 context。消息 ID 要无损支持 uint64，优先顶层 ID，再取首个有效 item ID；缺失 / 零值保留现有 fallback 去重，不伪造稳定引用键。
- 引用解析保留账号、会话和消息来源隔离；优先可信本地快照，不因引用恢复而跨会话读取附件。媒体失败、引用缺失或转发展开达到限制时，保留可用文本、顺序及失败占位，不能丢弃整条用户输入。
- `send_attachment` 仅用于 ClawBot / NapCat 的当前会话已登记资源：`cache_photo/`、`tts_audio/` 或 `/api/attachments/<id>`。模型文字、搜索结果 URL、任意磁盘路径不构成授权；成功工具结果只能通过明确的附件 metadata 登记。
- ClawBot 媒体下载沿用公网 HTTPS、DNS / 重定向 / 私网地址限制、大小限制与专用 HTTP 客户端。图片按 AES key 是否存在决定解密（图片级 hex key 优先），不能只看 `encrypt_type`；坏 key / 密文不能降级为明文，音频 / 文件 / 视频仍需先校验 key。
- NapCat 事件中的本地文件路径不可信，不能直接读取；资源解析须通过可信 action 响应。发送模型文本使用结构化 text 段，避免被当成 CQ 指令；不要把 ClawBot 的公网下载约束直接描述成 NapCat 的既有行为。
- 附件下载接口保持鉴权、`application/octet-stream`、attachment disposition、`nosniff` 与 sandbox；沿用附件 ID 校验和受限目录访问，不把来源文件名当成磁盘路径。涉及落盘时覆盖 Windows 文件关闭 / 重命名语义。
- 通道改动参考 `api/clawbot_*_test.go`、`api/napcat_*_test.go`、`client/clawbot_*_test.go`、`storage/*delivery*_test.go` 和附件测试；重点验证重复事件、断线 / 重启、确认后落盘失败、部分投递、媒体失败及跨会话引用边界。

### 前端

- 保持组件 / hooks / services / types 的职责边界，尤其不要把已拆分的聊天逻辑重新堆入 `ChatDetail.tsx`。
- TypeScript 启用 strict 和未使用符号检查；不要用宽泛 `any` 或关闭检查掩盖接口不一致。
- 遵循 `web/.prettierrc.json`：4 空格、单引号、无分号、120 列、ES5 尾逗号；CSS 延续相邻文件约定。
- 新增用户可见文案同步 `web/src/i18n/zh.ts` 和 `en.ts`，复用现有国际化、主题、Toast 与确认框机制。
- 动效复用 `motion/react` 与 `web/src/utils/motion.ts`；UI 改动需检查移动端、明暗主题、滚动、虚拟键盘和减少动态效果的可访问性。
- 不要假设构建产物有独立 JS/CSS 文件：Vite 自定义插件将脚本、样式和 logo 内联到 `index.html`，保留单独的 `sw.js`，且关闭 public 目录自动复制。新增静态资源或修改代码分割时检查实际构建输出。

## 数据与安全边界

默认数据目录如下（`-data` 可以替换整个根路径）：

```text
src/
  config.json
  cornerstone.log
  memory_extraction_prompt.txt
  prompts/          # 人设、头像及人设下的 memories.jsonl
  chats/            # 会话 JSONL：首行为元数据，其后为消息
  user_about/       # 用户资料、头像及 password.json
  cache_photo/
  tts_audio/
  reminders/
  idle_greetings/
  clawbot/         # inbox / outbox、游标、回执、引用缓存等持久化状态
  qqofficial/      # 官方 QQ 私聊入站去重回执（独立于聊天删除）
  attachments/     # 私有附件文件，使用 /api/attachments/<id> 引用
```

- `src/` 是运行时数据，不是源码；不要提交、清空或用测试覆盖真实用户数据。数据目录要求单服务实例独占，JSON 管理器的进程内锁不提供多进程协调。
- 附件孤儿清理仅在启动管理器、接收器与 worker 前运行（`CleanupAttachmentsBeforeStart`）；会话、队列、引用缓存和工具结果都可能持有附件。不要删除会话时立即删附件；损坏 / 不可读快照或符号链接应保守中止清理。
- 配置中可能包含 API keys、通道 token、TLS 路径等敏感信息。不要将真实凭据、聊天记录、密码文件、日志或私钥写入代码、测试夹具、文档或提交。
- 修改路径处理或文件接口时沿用现有 ID / 文件名校验，避免路径穿越；不要削弱请求体大小、鉴权和首次初始化限制。
- 修改 JSON / JSONL 格式时考虑旧数据加载、可选字段与向后兼容；不要静默丢弃用户历史数据。
- 不提交 `web/node_modules/`、`web/dist/`、本地二进制和其他生成文件。根 `.gitignore` 默认忽略 `*.md`，当前已为 README 和 `AGENTS.md` 添加例外；需要跟踪的其他新文档应添加明确例外。

## 发布与交付检查

- 当前发布工作流由 tag push 触发，构建 Linux / macOS / Windows 的 amd64 和 arm64，打包二进制与 `web/dist`，生成 `checksum.txt`。
- 前端版本通过 `VERSION` 环境变量注入；后端通过 `-ldflags="-X main.version=<tag>"` 注入。未注入时尝试精确匹配当前 Git tag，否则使用 `dev`。
- 发布工作流目前执行构建，不执行 Go 测试或前端格式检查；不要把发布成功视为全部验证通过。
- 开始修改前检查 `git status --short`，保留已有未提交工作；不要顺手回滚、格式化或提交无关文件。提交前检查 `git diff --check` 与变更范围（包括未跟踪文件），只保留任务相关改动。根据影响运行对应测试 / 构建，并如实说明未运行或失败的检查。
- UI 或交互变更还应手动验证相关流程；聊天核心变更应覆盖流式 / 非流式、工具调用、历史恢复及相关外部通道。不要把仅类型检查通过当作运行时验证。
