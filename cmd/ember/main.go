package main

import (
	"fmt"
	"os"

	"github.com/brandonhon/ember/internal/config"
)

// version is set via -ldflags "-X main.version=..." at build time.
var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-v" || os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Printf("ember %s\n", version)
		return
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ember: config error: %v\n", err)
		os.Exit(2)
	}
	fmt.Printf("ember %s — addr=%s db=%s\n", version, cfg.Addr, cfg.DBPath)
}
