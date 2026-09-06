# tix

`tix` is a barebones command-line ticket tracker. It stores tickets in a local SQLite database at `~/.tix/tix.db` and scopes work by project name.

## Install

```sh
go install github.com/brianseitel/tix@latest
```

For local development:

```sh
go build .
```

## Usage

Projects are ad-hoc. Passing a new `--project` value starts using that project.

```sh
tix add --project acme "fix login bug" --body "users cannot log in"
tix add --project acme "investigate search timeout" --body-file ./ticket.md
tix add --project acme "write docs" --body "README" --priority 0
tix list --project acme
tix projects
tix show 1 --json --compact
tix claim-next --project acme --json --compact
tix best-practices
tix init-agent acme
tix next --project acme
tix bump 1 0
tix done 1
tix rm 2 --project acme
```

## Commands

- `tix add [title] --project NAME (--body TEXT | --body-file PATH) [--priority N]`: add a ticket.
- `tix projects [--json] [--compact] [--fields FIELDS]`: list projects with ticket counts grouped by status.
- `tix list --project NAME [--all] [--status STATUS] [--priority N] [--limit N] [--json] [--compact] [--fields FIELDS]`: list tickets for a project. Done tickets are hidden unless `--all` is set.
- `tix next --project NAME [--json] [--compact] [--fields FIELDS]`: show the highest-priority open ticket for a project.
- `tix claim-next --project NAME [--json] [--compact] [--fields FIELDS]`: mark the highest-priority open ticket `in_progress` and print it.
- `tix show [id] [--json] [--compact] [--fields FIELDS]`: show full details for one ticket.
- `tix best-practices [--json] [--compact] [--fields FIELDS]`: show terse guidance for agent workflows.
- `tix init-agent PROJECT [--db PATH]`: append idempotent Tix workflow guidance to `AGENTS.md` and `CLAUDE.md` at the current Git repository root (or current directory).
- `tix bump [id] [priority]`: change a ticket's priority.
- `tix done [id]`: mark a ticket done.
- `tix rm [id] --project NAME`: remove a ticket from a project.

Priorities are sorted ascending, so `P0` appears before `P2`. Tickets with the same priority keep insertion order.

## Development

Run the unit tests:

```sh
go test ./...
```

Run the shell-based end-to-end checks:

```sh
./e2e.sh
```

Set `TIX_DB` to use a specific database path:

```sh
TIX_DB=.tix.db tix list --project acme
```
