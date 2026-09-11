# Contributing to tix

Thanks for taking an interest. `tix` is a small Go CLI, and it stays small on purpose — the
most useful contributions are focused ones.

By participating you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md).

## Getting set up

You need [Go](https://go.dev/dl/) 1.25 or newer. There is nothing else to install — the SQLite
driver is pure Go, so there is no cgo toolchain to configure.

```sh
git clone https://github.com/waygatetech/tix.git
cd tix
go build .
```

Run your build against a throwaway database rather than your real one:

```sh
TIX_DB=/tmp/tix-dev.db ./tix projects add scratch
TIX_DB=/tmp/tix-dev.db ./tix add --project scratch "try something" --body "..."
```

`TIX_DB` overrides the default `~/.tix/tix.db`, so nothing you do while developing touches the
tickets you actually care about.

## Checks to run before opening a pull request

These are the same three commands CI runs, so running them locally means no surprises:

```sh
go build ./...   # everything compiles
go test ./...    # the unit test suite
./e2e.sh         # shell-based end-to-end checks in an isolated temporary home
gofmt -l .       # prints nothing when formatting is correct
```

## Where code goes

- `cmd/` — user-facing CLI behavior: Cobra commands, flags, and output formatting.
- `internal/tix/` — reusable ticket and database behavior.
- `mcp/` — the MCP server exposed as `tix mcp`.
- `e2e.sh` — CLI workflow coverage that spans several commands.

A change that adds a command usually touches `cmd/` for the surface and `internal/tix/` for the
behavior underneath it. Keeping that split means the MCP server and the web dashboard get the
same behavior without duplicating it.

A few things worth knowing before you dig in:

- The `internal/tix` package uses a package-level `db`. Tests should reset or close it between
  cases so they do not leak state into each other.
- Any standalone binary or test package that opens a SQLite connection needs
  `_ "modernc.org/sqlite"` imported for its side effect.
- Commands that accept `--project` must keep their project scoping. It is the guarantee that one
  database can hold every project safely.

## Tests

Add focused Go tests next to the package you changed, and put multi-command CLI workflows in
`e2e.sh`. A bug fix should come with a test that fails without it.

## Style

- Standard `gofmt` formatting, no exceptions.
- Keep changes small. This repository intentionally has minimal abstractions; please do not add
  a layer of indirection to support a case nobody has hit yet.
- Match the surrounding code. Error messages in particular have a voice: they name what was
  wrong and what the real options are (see the unknown-project error for the pattern).

## Pull requests

- One concern per pull request. Two unrelated fixes are two pull requests.
- Say what the change does and why in the description. If it changes user-facing behavior,
  update `README.md` in the same pull request.
- Green CI is required: `go build ./...`, `go test ./...`, and `./e2e.sh` all run on every pull
  request.

If you are considering something large — a new subcommand, a schema change, a dependency —
open an issue first so we can talk it through before you spend the time.

## Reporting bugs and asking for features

Use the [issue templates](https://github.com/waygatetech/tix/issues/new/choose). For a bug, the
most useful thing you can include is the exact command you ran, what you expected, and what
happened instead, along with `tix --version` and your OS.
