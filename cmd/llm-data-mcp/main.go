package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samestrin/llm-data-axi/internal/data/mcpserver"
)

const (
	serverName         = "llm-data-mcp"
	serverVersion      = "1.0.0"
	serverInstructions = "llm-data MCP provides generic tools for directory analysis, file searching, YAML querying, git context and project detection. It is a fallback for environments that cannot run the llm-data CLI directly; the CLI is the primary interface."
)

func main() {
	// Verify the llm-data binary exists. The error names the path tried, the
	// lookup order and the LLM_DATA_BINARY override, so a broken install is
	// fixable from the error alone (epic 09 T4).
	if _, err := os.Stat(mcpserver.BinaryPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "ERROR: llm-data binary not found at %s\n", mcpserver.BinaryPath)
		fmt.Fprintf(os.Stderr, "The server looks for llm-data in LLM_DATA_BINARY, on PATH, beside the llm-data-mcp executable, and at %s.\n", mcpserver.FallbackBinaryPath)
		if sibling := mcpserver.SiblingBinaryPath(); sibling != "" {
			fmt.Fprintf(os.Stderr, "Beside this executable is %s.\n", sibling)
		}
		fmt.Fprintf(os.Stderr, "Install llm-data or set LLM_DATA_BINARY to its full path.\n")
		os.Exit(1)
	}

	// Create MCP server using official SDK
	server := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Version: serverVersion,
	}, &mcp.ServerOptions{
		Instructions: serverInstructions,
	})

	// Register all tools
	tools := mcpserver.GetToolDefinitions()
	for _, toolDef := range tools {
		// Capture for closure
		td := toolDef
		server.AddTool(&mcp.Tool{
			Name:        td.Name,
			Description: td.Description,
			InputSchema: td.InputSchema,
		}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			// Unmarshal arguments
			var args map[string]interface{}
			if req.Params.Arguments != nil {
				if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{
							&mcp.TextContent{Text: "Error parsing arguments: " + err.Error()},
						},
						IsError: true,
					}, nil
				}
			}

			// Execute the tool using the handler
			output, err := mcpserver.ExecuteHandler(td.Name, args)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{Text: "Error: " + err.Error()},
					},
					IsError: true,
				}, nil
			}
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: output},
				},
			}, nil
		})
	}

	// Log startup
	fmt.Fprintf(os.Stderr, "%s v%s started with %d tools\n", serverName, serverVersion, len(tools))

	// Run server on stdio
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}
