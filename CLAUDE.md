# Project

SSO system implementing an OAuth 2.1 / OpenID Connect provider. Early-stage monorepo: Go microservices backend, with Next.js frontend.

# Commands

# Architecture

- `api/` — the single source of truth for all microservice APIs: protobuf definitions in `api/proto/<service>/v1/*.proto`.
- `backend/` — Go workspace (`backend/go.work`, Go 1.27) with one module per deployable plus shared modules:
  - `backend/cmd/<service>/` — deployable gRPC services: `auth`, `oidc`, `users`, `clients`.
  - `backend/cmd/gateway/` — HTTP API gateway: exposes the public endpoints and calls the gRPC services above.
  - `backend/gen/` — generated protobuf/gRPC code (via `task gen` from `api/`). Never edit by hand.
  - `backend/pkg/` — shared modules (`logger` — slog-based logging with the `sl.Err(err)` helper and a pretty handler).
- `frontend/` — Next.js + TypeScript fronted. Not started yet.
- `infra/` — Infrastructure as Code (Docker, Terraform). Not started yet.
- `scripts/` — Helper scripts for development and CI. Not started yet.

# Conventions

- Commits follow Conventional Commits (`feat:`, `fix:`, `init:`, ...), lowercase, imperative.
- Logging via `log/slog` through `backend/pkg/logger`; attach errors with `sl.Err(err)`.
- Each service is its own module under `backend/cmd/<service>/` with its own `go.mod`; shared code goes to `backend/pkg/<name>/` and is registered in `backend/go.work`.
- New or changed APIs start as `.proto` in `api/`; regenerate Go code with `task gen` instead of editing `backend/gen/`.
