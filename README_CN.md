# CornerStone

[English](README.md) | 中文

CornerStone 是一个自托管的 AI 聊天客户端，后端使用 Go，前端使用 React + TypeScript。它把聊天会话、人设管理、记忆提取、朋友圈动态、生图、TTS 和本地数据持久化整合到同一个 Web 应用中。

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
- 支持带 AI 生图的朋友圈动态、点赞、评论和背景图
- 支持 MiniMax TTS
- 支持 ClawBot / 微信 iLink 配置与二维码登录流程
- 支持浏览器新消息通知
- 支持通过命令行参数或 `config.json` 启用 HTTPS

## 快速开始

### 前置要求

- Go `1.26.1`（来自 `go.mod`）
- 较新的 Node.js 与 npm 环境

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
    moments/
```

其中 `moments/` 用于存放朋友圈数据、生成图片和背景图；提示词、聊天记录、用户资料等也都会以本地 JSON 文件的形式保存在这棵目录树下。

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
- `api/`：HTTP 处理器、鉴权、聊天、记忆、朋友圈、TTS 与管理接口
- `client/`：上游模型提供商客户端
- `config/`：配置加载、默认值与校验
- `storage/`：提示词、聊天记录、用户数据、记忆和朋友圈的 JSON 持久化
- `web/`：React + TypeScript 前端源码与构建产物
- `src/`：默认运行时数据目录

## 贡献

欢迎提交贡献。请尽量保持修改最小化，遵循现有代码风格，并在提交 Pull Request 之前使用对应的构建或测试命令验证改动。

## 许可证

本项目基于 AGPL 3.0 许可证发布，详见 [LICENCE](LICENCE)。
