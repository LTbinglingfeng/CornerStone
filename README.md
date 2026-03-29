# CornerStone

English | [中文](README_CN.md)

CornerStone is a self-hosted AI chat client with a Go backend and a React + TypeScript frontend. It combines chat sessions, persona management, memory extraction, Moments-style posting, image generation, TTS, and local data persistence in one web application.

## Overview

- Single HTTP server for the backend API and the built frontend
- Local-first runtime data stored under a configurable data directory
- Username/password bootstrap authentication for management and chat endpoints
- Multi-provider support for chat, memory extraction, image generation, and TTS
- No external database required

## Features

- Streaming and non-streaming chat responses
- Persona and prompt management with avatar uploads
- Session history with editing, regeneration, recall, and per-persona conversations
- Multimodal chat with uploaded image cache
- Provider management for OpenAI-compatible Chat Completions, OpenAI Responses API, Google Gemini, Gemini image generation, and Anthropic Claude
- Dedicated memory extraction settings, provider override, refresh interval, and custom extraction template
- Moments feed with AI-generated images, likes, comments, and custom background images
- MiniMax TTS integration
- ClawBot / WeChat iLink settings and QR login flow
- Browser notifications for new replies
- Optional HTTPS via CLI flags or `config.json`

## Quick Start

### Prerequisites

- Go `1.26.1` (from `go.mod`)
- A recent Node.js and npm environment

### 1. Build the frontend

```bash
cd web
npm install
npm run build
```

### 2. Build the backend

```bash
go build -o cornerstone
```

Windows:

```powershell
go build -o cornerstone.exe
```

### 3. Run the server

```bash
./cornerstone -port 1205 -data ./src
```

Windows:

```powershell
.\cornerstone.exe -port 1205 -data .\src
```

Open `http://localhost:1205/`.

On first launch:

1. Complete the one-time username/password setup.
2. Add at least one chat provider in Settings.
3. Create a persona or start a session.

Notes:

- If `config.json` does not exist, CornerStone creates it automatically with default values.
- Auth tokens are stored in memory only and become invalid after a restart.
- If `web/dist` is missing, the backend still starts, but it will not serve the frontend UI.

## Development

Backend:

- `go test ./...`
- `go build -o cornerstone`

Frontend:

- `cd web && npm run dev`
- `cd web && npm run build`
- `cd web && npm run preview`

## Runtime Data

All runtime changes are persisted under the directory passed to `-data`. If omitted, the server uses `src` next to the executable and creates missing files automatically.

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

The `moments/` directory stores the feed data, generated images, and background images. Prompt definitions, chat history, and user profile data are also kept locally in JSON files under this tree.

## API and Access

- Frontend UI: `/`
- Chat API: `/api/chat`
- Management API: `/management`

All protected endpoints require authentication. The management API is documented in detail in the repository, while the chat endpoint stays under `/api/chat`.

## CLI Flags

- `-port`: server port, default `1205`
- `-data`: runtime data directory
- `-web`: frontend build directory, defaults to `web/dist` when available
- `-tls-cert`: TLS certificate path in PEM format
- `-tls-key`: TLS private key path in PEM format

HTTPS can also be enabled by setting `tls_cert_path` and `tls_key_path` in `config.json`.

## Project Structure

- `main.go`: backend entrypoint and static file registration
- `api/`: HTTP handlers, auth, chat, memory, moments, TTS, and management routes
- `client/`: upstream provider clients
- `config/`: configuration loading, defaults, and validation
- `storage/`: JSON persistence for prompts, chats, user data, memory, and moments
- `web/`: React + TypeScript frontend source and build output
- `src/`: default runtime data directory

## Contributing

Contributions are welcome. Keep changes minimal, follow the existing project style, and validate backend or frontend changes with the relevant build or test command before opening a pull request.

## License

This project is licensed under the AGPL 3.0 License. See [LICENCE](LICENCE) for details.
