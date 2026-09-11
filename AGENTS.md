# Repository Guidelines

## Project Overview

`tix` is a small Go CLI for managing project-scoped tickets in a local SQLite database. Cobra handles command routing in `cmd/`; core ticket behavior lives in `internal/tix`.

## Commands

- `go test ./...`: run the full Go test suite.
- `go build ./...`: verify all packages compile.
- `./e2e.sh`: run shell-based end-to-end checks against an isolated temporary home directory.

## Code Notes

- Keep user-facing CLI behavior in `cmd/` and reusable ticket/database behavior in `internal/tix/`.
- The database is resolved in `internal/tix/db.go`: `TIX_DB`, then the database a repo opts into with `.tix/config.json`, then `~/.tix/tix.db`. Set `TIX_DB` in tests or automation to avoid the real user database.
- Repo-local resolution is CLI-only; `tix mcp` takes `TIX_DB` or the global database, because an MCP server's working directory is not reliably the caller's repo.
- The `internal/tix` package uses a package-level `db`; tests should reset or close it between cases.
- Import `modernc.org/sqlite` for its side effect wherever a standalone binary or test package needs to open SQLite connections.
- Preserve project scoping for commands that accept `--project`.

## Style

- Follow standard Go formatting with `gofmt`.
- Prefer focused tests for package behavior and keep CLI workflow coverage in `e2e.sh`.
- Keep changes small; this repo intentionally has minimal abstractions.
