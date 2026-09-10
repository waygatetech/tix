# tix MCP server

`tix-mcp` exposes `tix`'s ticket operations as [MCP](https://modelcontextprotocol.io) tools over stdio, so agents like Claude Desktop can create, list, and edit tickets directly instead of shelling out to the `tix` CLI.

It calls the same `internal/tix` library functions the CLI uses, and talks to the same SQLite database (`~/.tix/tix.db` by default, or `TIX_DB` if set).

## Tools

| Tool             | Description                                                        |
| ---------------- | ------------------------------------------------------------------- |
| `tix_add`        | Create a new ticket in a project.                                  |
| `tix_list`       | List tickets in a project, optionally filtered by status/priority. |
| `tix_show`       | Show a single ticket by id.                                        |
| `tix_edit`       | Edit fields of an existing ticket.                                 |
| `tix_done`       | Mark a ticket as done.                                              |
| `tix_bump`       | Change a ticket's priority.                                        |
| `tix_rm`         | Delete a ticket from a project.                                    |
| `tix_next`       | Show the next open ticket to work on in a project.                 |
| `tix_claim_next` | Atomically claim the next open ticket in a project.                |
| `tix_projects`   | List all projects with ticket counts per status.                   |

Each tool returns compact JSON (both as structured content and as its text representation) rather than the CLI's human-formatted, colorized output.

## Build

```sh
go build -o tix-mcp ./mcp/cmd/tix-mcp
```

## Register with Claude Desktop

Add the built binary to Claude Desktop's config (`~/Library/Application Support/Claude/claude_desktop_config.json` on macOS):

```json
{
  "mcpServers": {
    "tix": {
      "command": "/absolute/path/to/tix-mcp",
      "args": []
    }
  }
}
```

To point it at a specific database instead of the default `~/.tix/tix.db`, set `TIX_DB` in the server's environment:

```json
{
  "mcpServers": {
    "tix": {
      "command": "/absolute/path/to/tix-mcp",
      "args": [],
      "env": {
        "TIX_DB": "/absolute/path/to/tix.db"
      }
    }
  }
}
```

Restart Claude Desktop after editing the config.
