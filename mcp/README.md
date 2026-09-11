# tix MCP server

`tix mcp` exposes `tix`'s ticket operations as [MCP](https://modelcontextprotocol.io) tools over stdio, so agents like Claude Desktop can create, list, and edit tickets directly instead of shelling out to the `tix` CLI.

It is a subcommand of the `tix` binary, so every released build already has it — nothing extra to install or compile.

It calls the same `internal/tix` library functions the CLI uses, and talks to the same SQLite database (`~/.tix/tix.db` by default, or `TIX_DB` if set).

## Tools

| Tool             | Description                                                        |
| ---------------- | ------------------------------------------------------------------- |
| `tix_add`        | Create a new ticket in an existing project (`create: true` makes a new one). |
| `tix_add_batch`  | Create several tickets in one project in a single call.            |
| `tix_list`       | List tickets in a project, optionally filtered by status/priority. |
| `tix_search`     | Find a project's tickets by a term in the title or body.           |
| `tix_show`       | Show a single ticket by id.                                        |
| `tix_edit`       | Edit fields of an existing ticket.                                 |
| `tix_projects`   | List all projects with ticket counts per status.                   |

`tix_search` is the one to reach for before filing: past about fifty tickets, nobody — least of all a client that files independently of whoever works the backlog — can hold the list in their head, and a duplicate costs more to notice later than the call costs now. It searches every status, because work that already shipped makes a new ticket just as redundant as an open one does.

That is the authoring half of the workflow and nothing else: write a ticket down, read it back, correct it. Claiming and closing belong to whoever does the work, so `claim-next`, `done`, and `rm` stay on the CLI — a client here would otherwise be able to close tickets it never touched. `tix_edit` can still set a status when a ticket genuinely needs one changed by hand, and it covers re-prioritizing too, which is why there is no separate `bump` tool.

## Batching

A brainstorm rarely ends with one ticket. `tix_add_batch` takes the whole list — `{"project": "tix", "tickets": [{"title": …, "body": …, "priority": …}, …]}` — and writes it in one round trip instead of one per ticket, which is where the latency and tokens actually go:

```json
{
  "project": "tix",
  "create": false,
  "tickets": [
    { "title": "Add tix_add_batch", "body": "Six tickets, six round trips.", "priority": 1 },
    { "title": "Document the batch tool", "body": "The README lists tools one per row." }
  ]
}
```

Every ticket in a batch lands in the same project; move one afterwards with `tix_edit` if it belongs elsewhere. The reply counts what landed and lists each new ticket:

```
created 2 in tix
84 [P1] Add tix_add_batch
85 [P2] Document the batch tool
```

A batch is validated whole before anything is written, so a missing title in the fifth ticket rejects the call naming `tickets[4]` and leaves no half-written batch behind — and no newly created project either, even with `create: true`. If a write does fail partway (a database error rather than bad input), the error says how many tickets already landed and the payload still lists them, so the retry can skip those instead of duplicating them.

The `project` argument of every tool is resolved against the projects that exist, ignoring case and punctuation: `Front-End` reaches the project named `frontend`, and a name that matches nothing is an error listing the real project names. That is what keeps an invented spelling from becoming a fifth project whose tickets nobody else sees.

The server also hands the client its instructions at connect time, and they carry the project list read when the server started, plus the rule to use one of those names. A client that can see the names has no reason to guess at one, which turns fragmentation from something the server rejects after the fact into something the client does not attempt. Projects created after the server started are not in that list — `tix_projects` answers live.

## Replies are one block of plain text

Each tool returns a single text block in the CLI's formatting — `78 [P1] title` for a listing, `78 [P1] title (open, tix)` plus the body for one ticket, `tix open=5 total=5` per project — and nothing else.

It used to return the whole payload as JSON *twice*, once as the text block and once as `structuredContent`, with a generated `outputSchema` on every tool to describe it. Nothing consumed the structured half, and the schemas sat in context for the entire session. Dropping both, measured against this repo's own backlog:

| | Before | After |
| --- | --- | --- |
| `tools/list` + instructions (every session) | 6,052 bytes ≈ 1,354 tokens | **3,955 bytes ≈ 869 tokens** |
| `tix_list`, 5 tickets | 1,440 bytes | **328 bytes** |
| `tix_show` | 348 bytes | **120 bytes** |
| `tix_projects` | 236 bytes | **57 bytes** |

Ids print bare rather than with the CLI's project prefix: these tools take an integer `id`, so `78` is what a client can hand back to `tix_show`, and `ti-78` is not. A client that genuinely needs to parse replies should use the CLI's `--json`, which is still there for programs.

## Register with Claude Desktop

Install `tix` (see the [main README](../README.md#install)), then point Claude Desktop's config (`~/Library/Application Support/Claude/claude_desktop_config.json` on macOS) at the binary with the `mcp` subcommand:

```json
{
  "mcpServers": {
    "tix": {
      "command": "/absolute/path/to/tix",
      "args": ["mcp"]
    }
  }
}
```

Use an absolute path — Claude Desktop does not inherit your shell's `PATH`. `which tix` prints it.

Unlike the CLI, the server does not look for a repo's `.tix/config.json`: its working directory is wherever the client launched it, which is not reliably the repo the calling agent means — the same reason its tools require an explicit `project`. To point it at a specific database instead of the default `~/.tix/tix.db`, set `TIX_DB` in the server's environment:

```json
{
  "mcpServers": {
    "tix": {
      "command": "/absolute/path/to/tix",
      "args": ["mcp"],
      "env": {
        "TIX_DB": "/absolute/path/to/tix.db"
      }
    }
  }
}
```

Restart Claude Desktop after editing the config.
