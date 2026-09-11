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

Projects are created explicitly, so a mistyped `--project` fails instead of quietly starting a new project that hides your tickets.

```sh
# create a project along with its first ticket
tix add --project acme "fix login bug" --body "users cannot log in" --create

# or create it up front
tix projects add acme

# add a ticket to a project that exists
tix add --project acme "fix search" --body "timeouts over 30s"

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

- `tix add [title] --project NAME (--body TEXT | --body-file PATH) [--priority N] [--create]` — add a ticket. Priority defaults to `2`; lower numbers are higher priority (`P0` is most urgent). `--create` makes the project when it does not exist yet.
- `tix list --project NAME [--all] [--status STATUS] [--priority N] [--limit N] [--json] [--compact] [--fields FIELDS]` — list tickets for a project. Done tickets are hidden unless `--all` is set.
- `tix next --project NAME [--json] [--compact] [--fields FIELDS]` — show the highest-priority open ticket for a project.
- `tix claim-next --project NAME [--json] [--compact] [--fields FIELDS]` — mark the highest-priority open ticket `in_progress` and print it.
- `tix show [id] [--json] [--compact] [--fields FIELDS]` — show full details for one ticket.
- `tix projects [--json] [--compact] [--fields FIELDS]` — list all projects with ticket counts grouped by status.
- `tix projects add NAME [--key PREFIX] [--repo-path PATH]` — create a project. `--key` pins its ticket ID prefix; `--repo-path` records where its code lives.
- `tix projects set-repo-path NAME [PATH]` — record where an existing project's code lives, so commands run there can skip `--project`. `PATH` defaults to the current directory.
- `tix bump [id] [priority]` — change a ticket's priority.
- `tix done [id]` — mark a ticket done.
- `tix rm [id] --project NAME` — remove a ticket from a project.
- `tix prefix [project] [prefix]` — show ticket ID prefixes, or pin one for a project.
- `tix best-practices [--json] [--compact] [--fields FIELDS]` — print terse workflow guidance, handy for scripting or onboarding an AI agent.
- `tix mcp` — run an MCP server on stdio exposing the ticket operations as tools, for MCP clients like Claude Desktop. See [mcp/README.md](mcp/README.md).
- `tix init-agent PROJECT [--db PATH]` — append idempotent `tix` workflow guidance to `AGENTS.md` and `CLAUDE.md` at the current Git repository root (or the current directory), so coding agents know how to use `tix` in this repo.

Priorities sort ascending, so `P0` appears before `P2`. Tickets with the same priority keep insertion order.

### Projects

A project is a row, not whatever string you happened to type. Every command that takes `--project` resolves the name against the projects that exist, and an unknown one is an error listing the names that are real:

```
$ tix list --project frontned
unknown project "frontned": known projects are acme, frontend, zen; pass --create (or create=true) to make it a new one
```

Matching ignores case and punctuation, so `frontend`, `Frontend`, `front-end` and `FRONT END` all reach the one project named `frontend` rather than starting three more. Creating a project whose name only varies that way is refused for the same reason.

This matters most for tools with no shell history to autocomplete from: an agent inventing a spelling gets an error naming the real projects instead of a private backlog nobody else can see.

### Inferring the project from the working directory

Tell a project where its code lives, and commands run inside that checkout stop needing `--project` at all:

```sh
tix projects set-repo-path acme ~/src/acme

cd ~/src/acme/internal/search
tix next          # the acme backlog
tix add "fix search" --body "timeouts over 30s"
```

Any directory inside the repository counts, and the deepest registered repo wins, so a project checked out inside another one answers for its own tree. `--project` still works everywhere and always wins, which keeps filing into another repo's backlog something you say out loud. Outside every registered repo the flag is required, and the error says which directory found no match.

### Ticket IDs

One database holds every project, so ticket IDs are unique across all of them. Output displays them with a short project prefix, which tells you which repo a ticket belongs to without a lookup:

```
fe-47 [P2] fix login redirect
```

Any command taking an ID accepts either form — `tix done 47` and `tix done fe-47` both work, so IDs already in shell history and scripts stay valid. The difference is that the prefixed form is checked: if ticket 47 is not a `fe` ticket, the command fails instead of acting on the wrong project's ticket.

The ID is an integer everywhere it is consumed by a program. `--json` output and the MCP tools emit `"id": 47`, never the prefixed string, so the prefix is purely a display and input convenience.

Prefixes are derived from project names automatically — the shortest unique lowercase prefix of the name. Adding a project whose name collides can shift another project's derived prefix, so pin one when you want it fixed:

```sh
tix prefix              # list every project's prefix
tix prefix frontend fe  # pin it
```

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

`tix` is created and maintained by Waygate Technologies, LLC.

Copyright © 2026 Waygate Technologies, LLC.

Licensed under the [Apache License, Version 2.0](LICENSE). See [NOTICE](NOTICE) for attribution
requirements when redistributing.
