package cmd

import (
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	tixmcp "github.com/waygatetech/tix/mcp"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run an MCP server exposing tix ticket operations over stdio",
	Long: `Run an MCP server exposing tix ticket operations over stdio.

The server speaks the Model Context Protocol on stdin/stdout, so MCP clients
like Claude Desktop can call tix operations as tools. It reads and writes the
same database the CLI uses (~/.tix/tix.db by default, or TIX_DB if set).`,
	PersistentPreRunE: initDB,
	RunE: func(cmd *cobra.Command, args []string) error {
		return tixmcp.NewServer().Run(cmd.Context(), &mcpsdk.StdioTransport{})
	},
}

func init() {
	rootCmd.AddCommand(mcpCmd)
}
