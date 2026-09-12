# Task Wizard — Architecture Summary

Task Wizard is a self-hosted, privacy-focused task management application. It is composed of four main components.

## System Components

### 1. API Server (`apiserver/`)
- **Language**: Go
- **Framework**: Gin (HTTP), WebSocket
- **DI**: Uber FX
- **Database**: PostgreSQL (default, first-class) or SQLite (local dev) via GORM. Supports up to three DSN-backed pools (primary RW, read R, read-only replica RO) with automatic fallback.
- **Role**: The central backend. Handles all business logic, persistence, authentication, background scheduling, notifications, and serves the frontend as static files.

### 2. Frontend (`frontend/`)
- **Language**: TypeScript
- **Framework**: React (class components) with Redux
- **Transport**: HTTP REST
- **Role**: Single-page application for task management, label organization, notification configuration, and user settings.

### 3. Android App (`android/`)
- **Language**: Kotlin
- **Framework**: Jetpack Compose, Material 3
- **DI**: Hilt
- **Network**: Retrofit (REST), OkHttp (WebSocket)
- **Auth**: MSAL (Microsoft Entra ID)
- **Role**: Native Android client for task and label management with real-time WebSocket sync.

### 4. MCP Server (`mcpserver/`)
- **Language**: C# (.NET 9)
- **Framework**: ASP.NET Core with ModelContextProtocol
- **Role**: Exposes task and label management as MCP (Model Context Protocol) tools so AI assistants can interact with Task Wizard programmatically. Proxies all requests to the real API server via `ApiProxyService` (configurable via `TW_API_URL`). Authenticates using Microsoft Entra ID.


## Communication

```
┌───────────┐    HTTP REST       ┌──────────────┐
│  Frontend │ ──────────────────► │  API Server  │
│  (React)  │ ◄──── WebSocket ── │  (Go / Gin)  │
└───────────┘                    └──────┬───────┘
                                       │ GORM
┌───────────┐    HTTP REST             ▼
│  Android  │ ──────────────────► ┌────────────┐
│ (Kotlin)  │ ◄──── WebSocket ── │  PostgreSQL │
└───────────┘                    │  (or SQLite)│
                                 └────────────┘

┌───────────┐  MCP (HTTP)
│ AI Client │ ──────────────────► ┌─────────────┐
└───────────┘                    │ MCP Server  │
                                 │ (.NET)      │
                                 └─────────────┘
```

## API Server Internals

| Layer | Directory | Purpose |
|-------|-----------|---------|
| HTTP Handlers | `internal/apis/` | REST route handlers |
| Middleware | `internal/middleware/` | Entra ID token validation, scope enforcement |
| Models | `internal/models/` | GORM data models |
| Repositories | `internal/repos/` | Database access layer |
| Services | `internal/services/` | Business logic (tasks, labels, users), scheduler, notifications, logging |
| Utilities | `internal/utils/` | Entra ID auth helpers, DB setup, test utilities |
| WebSocket | `internal/ws/` | Real-time push to connected clients |
| Migrations | `internal/migrations/` | Schema versioning |
| Config | `config/` | YAML-based configuration with env var overrides |

## Frontend Internals

| Layer | Directory | Purpose |
|-------|-----------|---------|
| Views | `src/views/` | Page-level React components (Tasks, Labels, Settings, Auth, etc.) |
| Store | `src/store/` | Redux slices for tasks, labels, user, feature flags, WebSocket, status |
| API | `src/api/` | HTTP transport abstraction |
| Models | `src/models/` | TypeScript interfaces and helpers |
| Components | `src/components/` | Shared UI components (ErrorBoundary, StatusList) |
| Utilities | `src/utils/` | Utility modules (MSAL auth, WebSocket client, date/color/grouping helpers, sound) |
| Contexts | `src/contexts/` | React contexts (RouterContext) |
| Constants | `src/constants/` | App constants (theme config, feature flag definitions, time constants) |

## Key Architectural Patterns

- **Repository pattern** for data access abstraction
- **Service layer** for business logic separation
- **Dependency injection** (Uber FX) for wiring
- **Scope-based authorization** via Entra ID scopes (e.g. `Tasks.Read`, `Labels.Write`)
- **Background scheduler** for notification generation, sending, and cleanup
- **Smart transport** in the frontend — uses WebSocket for real-time updates, HTTP for requests
- **Feature flags** infrastructure in the frontend (definitions, Redux slice, settings UI) — no flags currently defined
- **Real-time sync** via WebSocket with per-user connection tracking
