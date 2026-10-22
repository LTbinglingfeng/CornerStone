# CornerStone

[English](README.md) | 中文

CornerStone 是一个自托管的 AI 聊天客户端，后端使用 Go，前端使用 React + TypeScript。它把聊天会话、人设管理、记忆提取、TTS 和本地数据持久化整合到同一个 Web 应用中。

## 项目概览

- 使用单个 HTTP 服务同时提供后端 API 和前端静态页面
- 运行时数据保存在可配置的数据目录中
- 管理接口与聊天接口都使用用户名/密码初始化鉴权
- 支持为聊天、记忆提取、生图和 TTS 分别配置模型提供商
- 不依赖外部数据库

## 功能特性

- 支持流式与非流式聊天响应
- 支持人设 / 提示词管理与头像上传
- 支持会话历史、消息编辑、重新生成、撤回，以及按人设分组的对话
- 支持图片上传缓存的多模态聊天
- 支持 OpenAI 兼容 Chat Completions、OpenAI Responses API、Google Gemini、Gemini 生图和 Anthropic Claude
- 支持独立的记忆提取设置、专用模型、刷新间隔和自定义提取模板
- 支持 MiniMax TTS
- 支持 ClawBot / 微信 iLink 配置与二维码登录流程
- 支持浏览器新消息通知
- 支持通过命令行参数或 `config.json` 启用 HTTPS

## Gemini 供应商说明

- Gemini 聊天供应商和独立记忆供应商提供「自动续传」开关，默认关闭。开启后可能发起多次计费请求，仍受已有请求超时限制；聊天继续使用 `GenerateContent`。
- 生图客户端使用 Interactions API（`/interactions`），要求供应商和图片模型支持该协议。每次请求一张图，旧张数配置保留但不自动增加请求；当前 SDK 仅支持显式指定 JPEG，旧 PNG 配置需在 UI 中手动改为 JPEG 后再保存。生图请求关闭云端交互存储和自动重试。供应商配置和客户端本身不会新增聊天生图工具或投递入口。

## 快速开始

### 前置要求

- Go `1.26.10`（来自 `go.mod`）
- Node.js 24 LTS 与 npm

### 1. 构建前端

```bash
cd web
npm install
npm run build
```

### 2. 构建后端

```bash
go build -o cornerstone
```

Windows：

```powershell
go build -o cornerstone.exe
```

### 3. 启动服务

```bash
./cornerstone -port 1205 -data ./src
```

Windows：

```powershell
.\cornerstone.exe -port 1205 -data .\src
```

然后访问 `http://localhost:1205/`。

首次启动建议按下面顺序操作：

1. 完成一次性的用户名 / 密码初始化。
2. 在设置中添加至少一个聊天提供商。
3. 创建一个人设，或直接新建会话开始聊天。

说明：

- 如果 `config.json` 不存在，CornerStone 会自动创建默认配置。
- 鉴权令牌只保存在内存中，服务重启后会失效。
- 如果没有生成 `web/dist`，后端仍然可以启动，但不会提供前端页面。

## 开发命令

后端：

- `go test ./...`
- `go build -o cornerstone`

前端：

- `cd web && npm run dev`
- `cd web && npm run build`
- `cd web && npm run preview`

## 运行时数据目录

所有运行时变更都会落盘到 `-data` 指定的目录。如果未指定，程序默认使用可执行文件旁边的 `src` 目录，并自动创建缺失文件。

```text
src/
    config.json
    cornerstone.log
    memory_extraction_prompt.txt
    prompts/
    chats/
    user_about/
    cache_photo/
    tts_audio/
```

提示词、聊天记录、用户资料等都会以本地 JSON 文件的形式保存在这棵目录树下。

## 日志

应用日志同时输出到控制台和数据目录下的 `cornerstone.log`：

- `http.request.*`：API 请求开始/结束、方法、路径、HTTP 状态与耗时，不记录 query、请求头或响应正文。
- `chat.request.*` / `channel.request.*`：来源、触发方式、请求关联 ID 与处理结果；`generation.*`、`model.*`、`tool.*` 记录模型和工具各阶段。
- `model.completed`：模型、耗时、输入/输出/总 tokens；工具循环汇总所有模型轮次。`usage=unavailable` 表示未取得完整用量，`partial` 表示只有部分调用报告用量，不代表零消耗。Anthropic 日志输入总量包含缓存读写，原 API 响应的 usage 语义不变。
- `history.*` / `channel.history.*` 与 `channel.delivery.*` 分别记录落盘和投递。`server_written` 仅表示写出 HTTP 响应，不是浏览器确认；`queued`、`uncertain`、`partial` 不表示全部送达。
- `persona.*`、`memory.*`、`provider.*`、`config.updated`、`reminder.*`、`idle_greeting.*` 记录成功保存后的业务变化，只包含 ID、字段名、计数和时间，不包含设定/记忆正文或密钥值。

按 `request` 关联一次处理；ClawBot 持久化任务使用稳定的请求 ID 和 `job_id`，`scope=delivery_phase` 的耗时只覆盖当前队列阶段。当前 Web SSE 使用非流式上游模型调用，`first_text_ms` 是首次向前端写出可见文本的耗时，不是上游 token 首包时间。

日志仍采用追加文本文件，本版本未新增轮转或日志管理页面。分享日志前仍应检查并脱敏会话 ID、模型名称等元数据。

## API 与访问入口

- 前端页面：`/`
- 聊天接口：`/api/chat`
- 管理接口：`/management`

所有受保护接口都需要鉴权。仓库内已提供完整的管理 API 文档，而聊天接口保持在 `/api/chat` 路径下。

## 命令行参数

- `-port`：服务端口，默认 `1205`
- `-data`：运行时数据目录
- `-web`：前端构建目录，默认优先查找 `web/dist`
- `-tls-cert`：PEM 格式 TLS 证书路径
- `-tls-key`：PEM 格式 TLS 私钥路径

也可以在 `config.json` 中通过 `tls_cert_path` 与 `tls_key_path` 启用 HTTPS。

## 项目结构

- `main.go`：后端入口与前端静态文件注册
- `api/`：HTTP 处理器、鉴权、聊天、记忆、TTS 与管理接口
- `client/`：上游模型提供商客户端
- `config/`：配置加载、默认值与校验
- `storage/`：提示词、聊天记录、用户数据和记忆的 JSON 持久化
- `web/`：React + TypeScript 前端源码与构建产物
- `src/`：默认运行时数据目录

## 贡献

欢迎提交贡献。请尽量保持修改最小化，遵循现有代码风格，并在提交 Pull Request 之前使用对应的构建或测试命令验证改动。

## 许可证

本项目基于 AGPL 3.0 许可证发布，详见 [LICENCE](LICENCE)。
