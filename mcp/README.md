# tix MCP server

`tix mcp` exposes `tix`'s ticket operations as [MCP](https://modelcontextprotocol.io) tools over stdio, so agents like Claude Desktop can create, list, and edit tickets directly instead of shelling out to the `tix` CLI.

It is a subcommand of the `tix` binary, so every released build already has it — nothing extra to install or compile.

It calls the same `internal/tix` library functions the CLI uses, and talks to the same SQLite database (`~/.tix/tix.db` by default, or `TIX_DB` if set).

## Tools

| Tool             | Description                                                        |
| ---------------- | ------------------------------------------------------------------- |
| `tix_add`        | Create a new ticket in an existing project (`create: true` makes a new one). |
| `tix_list`       | List tickets in a project, optionally filtered by status/priority. |
| `tix_show`       | Show a single ticket by id.                                        |
| `tix_edit`       | Edit fields of an existing ticket.                                 |
| `tix_done`       | Mark a ticket as done.                                              |
| `tix_bump`       | Change a ticket's priority.                                        |
| `tix_rm`         | Delete a ticket from a project.                                    |
| `tix_next`       | Show the next open ticket to work on in a project.                 |
| `tix_claim_next` | Atomically claim the next open ticket in a project.                |
| `tix_projects`   | List all projects with ticket counts per status.                   |
| `tix_project_add`| Create a new project.                                              |

The `project` argument of every tool is resolved against the projects that exist, ignoring case and punctuation: `Front-End` reaches the project named `frontend`, and a name that matches nothing is an error listing the real project names. That is what keeps an invented spelling from becoming a fifth project whose tickets nobody else sees.

Each tool returns compact JSON (both as structured content and as its text representation) rather than the CLI's human-formatted, colorized output.

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

To point it at a specific database instead of the default `~/.tix/tix.db`, set `TIX_DB` in the server's environment:

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
