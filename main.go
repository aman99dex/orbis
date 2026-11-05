// Command orbis is a local-first context store for humans and coding agents.
//
//	orbis [-root DIR]       open the desktop app
//	orbis mcp [-root DIR]   run the MCP server on stdio (for Claude Code, Cursor, …)
//	orbis export FILE.zip [-root DIR]
//	orbis import FILE.zip [-root DIR]
//	orbis version
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"orbis/internal/index"
	"orbis/internal/mcpserver"
	"orbis/internal/portable"
	"orbis/internal/vault"
)

var version = "dev" // set with -ldflags "-X main.version=…"

func main() {
	log.SetFlags(0)
	log.SetPrefix("orbis: ")
	var err error
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "", "-root", "--root":
		fs := flag.NewFlagSet("orbis", flag.ExitOnError)
		root := fs.String("root", defaultRoot(), "Orbis folder")
		fs.Parse(os.Args[1:])
		err = runGUI(*root)
	case "mcp":
		err = runMCP(os.Args[2:])
	case "export", "import":
		err = runPortable(cmd, os.Args[2:])
	case "version":
		fmt.Println(version)
	default:
		usage()
	}
	if err != nil {
		log.Fatal(err)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage:
  orbis [-root DIR]                  desktop app
  orbis mcp [-root DIR]              MCP server on stdio for coding agents
  orbis export FILE.zip [-root DIR]  pack this Orbis into one zip
  orbis import FILE.zip [-root DIR]  merge a zip in (never overwrites)
  orbis version
default root: %s
`, defaultRoot())
	os.Exit(2)
}

// defaultRoot is $ORBIS_ROOT, else ~/Orbis.
func defaultRoot() string {
	if r := os.Getenv("ORBIS_ROOT"); r != "" {
		return r
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "Orbis"
	}
	return filepath.Join(home, "Orbis")
}

// open opens the vault and index; sync=false leaves the initial Sync to the
// caller (the GUI runs it in the background so the window shows at once).
func open(root string, sync bool) (*index.Index, error) {
	v, err := vault.Open(root)
	if err != nil {
		return nil, err
	}
	ix, err := index.Open(v)
	if err != nil {
		return nil, err
	}
	if !sync {
		return ix, nil
	}
	if _, err := ix.Sync(); err != nil {
		log.Printf("initial sync: %v", err) // keep serving; the watcher and next start heal it
	}
	return ix, nil
}

func runMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	root := fs.String("root", defaultRoot(), "Orbis folder")
	fs.Parse(args)

	ix, err := open(*root, true)
	if err != nil {
		return err
	}
	defer ix.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go ix.Watch(ctx, nil) // pick up edits made by humans or other agents
	return mcpserver.New(ix, version, mcpserver.MemoryFromEnv()).Run(ctx, &mcp.StdioTransport{})
}

func runPortable(cmd string, args []string) error {
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	root := fs.String("root", defaultRoot(), "Orbis folder")
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		usage()
	}
	file := args[0]
	fs.Parse(args[1:])
	ix, err := open(*root, true)
	if err != nil {
		return err
	}
	defer ix.Close()
	if cmd == "export" {
		n, err := portable.Export(ix.V, file)
		if err == nil {
			fmt.Printf("exported %d files to %s\n", n, file)
		}
		return err
	}
	res, err := portable.Import(ix.V, file)
	if err != nil {
		return err
	}
	if _, err := ix.Sync(); err != nil {
		return err
	}
	fmt.Printf("added %d, unchanged %d, kept both copies of %d\n", res.Added, res.Unchanged, len(res.Renamed))
	for _, r := range res.Renamed {
		fmt.Println("  " + r)
	}
	return nil
}
