# tix

[![test](https://github.com/waygatetech/tix/actions/workflows/test.yml/badge.svg)](https://github.com/waygatetech/tix/actions/workflows/test.yml)
[![release](https://img.shields.io/github/v/release/waygatetech/tix)](https://github.com/waygatetech/tix/releases/latest)
[![go.mod](https://img.shields.io/github/go-mod/go-version/waygatetech/tix)](go.mod)
[![license](https://img.shields.io/github/license/waygatetech/tix)](LICENSE)

`tix` is a lightweight command-line ticket tracker for solo devs and small teams. It stores tickets in a local SQLite database (`~/.tix/tix.db` by default) and scopes work by project, so one database can track tickets for every project you touch.

No server, no account, no config file — just a single binary and a SQLite file.

## Install

### Prebuilt binary (no Go required)

Download the archive for your platform from the [latest release](https://github.com/waygatetech/tix/releases/latest), extract it, and put the `tix` binary on your `PATH`, e.g.:

```sh
# macOS (Apple Silicon) example — swap in the archive for your OS/arch
curl -sL https://github.com/waygatetech/tix/releases/latest/download/tix_darwin_arm64.tar.gz | tar xz
sudo mv tix /usr/local/bin/
```

Binaries are published for macOS, Linux, and Windows (amd64 and arm64).

### With Go

Requires [Go](https://go.dev/dl/) 1.25+.

```sh
go install github.com/waygatetech/tix@latest
```

Make sure `$(go env GOPATH)/bin` is on your `PATH` so the `tix` binary is available in your shell.

To build from source instead:

```sh
git clone https://github.com/waygatetech/tix.git
cd tix
go build .
```

## Quick start

Projects are created implicitly — just pass a `--project` name and `tix` handles the rest.

```sh
# add a ticket
tix add --project acme "fix login bug" --body "users cannot log in"

# add a ticket with a longer body from a file
tix add --project acme "investigate search timeout" --body-file ./notes.md

# see what's outstanding
tix list --project acme

# see what to work on next
tix next --project acme

# claim it (marks it in_progress) and get the details
tix claim-next --project acme

# wrap up
tix done 1
```

## Commands

- `tix add [title] --project NAME (--body TEXT | --body-file PATH) [--priority N]` — add a ticket. Priority defaults to `2`; lower numbers are higher priority (`P0` is most urgent).
- `tix list --project NAME [--all] [--status STATUS] [--priority N] [--limit N] [--json] [--compact] [--fields FIELDS]` — list tickets for a project. Done tickets are hidden unless `--all` is set.
- `tix next --project NAME [--json] [--compact] [--fields FIELDS]` — show the highest-priority open ticket for a project.
- `tix claim-next --project NAME [--json] [--compact] [--fields FIELDS]` — mark the highest-priority open ticket `in_progress` and print it.
- `tix show [id] [--json] [--compact] [--fields FIELDS]` — show full details for one ticket.
- `tix projects [--json] [--compact] [--fields FIELDS]` — list all projects with ticket counts grouped by status.
- `tix bump [id] [priority]` — change a ticket's priority.
- `tix done [id]` — mark a ticket done.
- `tix rm [id] --project NAME` — remove a ticket from a project.
- `tix best-practices [--json] [--compact] [--fields FIELDS]` — print terse workflow guidance, handy for scripting or onboarding an AI agent.
- `tix mcp` — run an MCP server on stdio exposing the ticket operations as tools, for MCP clients like Claude Desktop. See [mcp/README.md](mcp/README.md).
- `tix init-agent PROJECT [--db PATH]` — append idempotent `tix` workflow guidance to `AGENTS.md` and `CLAUDE.md` at the current Git repository root (or the current directory), so coding agents know how to use `tix` in this repo.

Priorities sort ascending, so `P0` appears before `P2`. Tickets with the same priority keep insertion order.

### JSON output

Most read commands support `--json` for scripting, plus `--compact` (single-line JSON) and `--fields` (comma-separated field list) to trim the output down to what you need:

```sh
tix list --project acme --json --fields id,title,priority
tix show 1 --json --compact
```

### Using a custom database path

Set `TIX_DB` to point `tix` at a specific SQLite file instead of the default `~/.tix/tix.db`:

```sh
TIX_DB=.tix/tix.db tix list --project acme
```

This is useful for keeping a project-local ticket database checked into a repo, or for isolating test runs.

## Using tix with AI coding agents

`tix` works well as a lightweight task queue for AI coding agents. Run `tix init-agent PROJECT` in a repo to add ready-to-use `tix` guidance to `AGENTS.md`/`CLAUDE.md`, or point an agent at `tix best-practices` for a terse cheat sheet it can follow (claim the next ticket, do the work, mark it done, repeat).

`tix` also ships an MCP server as the `tix mcp` subcommand, so agents like Claude Desktop can call ticket operations directly as tools instead of shelling out to the CLI. No extra binary to install — see [mcp/README.md](mcp/README.md).

## Development

```sh
go test ./...      # run the unit test suite
go build ./...      # verify everything compiles
./e2e.sh             # run shell-based end-to-end checks
```

Contributions are welcome — keep changes small and focused, and follow standard `gofmt` formatting.

### Cutting a release

Pushing a version tag builds and publishes binaries automatically via [GoReleaser](https://goreleaser.com) and GitHub Actions:

```sh
git tag v0.1.0
git push origin v0.1.0
```

## License

`tix` is licensed under the [MIT License](LICENSE).
