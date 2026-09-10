// Command tix-mcp runs an MCP server exposing tix ticket operations over stdio.
package main

import (
	"context"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/waygatetech/tix/internal/tix"
	tixmcp "github.com/waygatetech/tix/mcp"
	_ "modernc.org/sqlite"
)

func main() {
	tix.InitDB()

	server := tixmcp.NewServer()
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("tix-mcp: server failed: %v", err)
	}
}
