package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	mcpsrv "github.com/GetModus/wraith/internal/mcp"
)

const version = "0.2.0"

func main() {
	vaultDir, dataDir := resolveDirs()

	if err := os.MkdirAll(vaultDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "cannot create vault directory %s: %v\n", vaultDir, err)
		os.Exit(1)
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "cannot create data directory %s: %v\n", dataDir, err)
		os.Exit(1)
	}

	log.SetOutput(os.Stderr)
	log.SetPrefix("[wraith-mcp] ")
	log.Printf("starting wraith-mcp %s — vault: %s data: %s", version, vaultDir, dataDir)

	srv := mcpsrv.NewServer("wraith-mcp", version)
	mcpsrv.RegisterWraithTools(srv, vaultDir, dataDir)
	srv.Run()
}

func resolveDirs() (string, string) {
	home, _ := os.UserHomeDir()

	vaultDir := os.Getenv("MODUS_VAULT_DIR")
	if vaultDir == "" {
		vaultDir = filepath.Join(home, "modus", "vault")
	}

	dataDir := os.Getenv("MODUS_DATA_DIR")
	if dataDir == "" {
		dataDir = filepath.Join(home, "modus", "data")
	}

	return vaultDir, dataDir
}
