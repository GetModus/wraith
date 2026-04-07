package wraith

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/GetModus/wraith/internal/markdown"
)

// IngestAudible scans a directory for Audible highlight/annotation exports
// (.md or .txt files) and ingests them into the vault.
// This replaces the Python ingest-audible.py which called the Archive API.
// In Go, we write directly to vault markdown files.
func IngestAudible(vaultDir string, state *State, highlightsDir string, maxItems int) (int, error) {
	if highlightsDir == "" {
		highlightsDir = filepath.Join(os.Getenv("HOME"), "modus", "data", "audible")
	}

	if _, err := os.Stat(highlightsDir); os.IsNotExist(err) {
		return 0, nil // No audible directory — skip silently
	}

	entries, err := os.ReadDir(highlightsDir)
	if err != nil {
		return 0, fmt.Errorf("audible read dir: %w", err)
	}

	ingested := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".md") && !strings.HasSuffix(name, ".txt") {
			continue
		}

		externalID := "audible:" + name
		if state.Exists("audible", externalID) {
			continue
		}

		fullPath := filepath.Join(highlightsDir, name)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			log.Printf("wraith: audible read error %s: %v", name, err)
			continue
		}

		content := string(data)
		title := strings.TrimSuffix(name, filepath.Ext(name))

		// Try to parse as markdown with frontmatter
		doc, parseErr := markdown.Parse(content)
		if parseErr == nil && doc.Get("title") != "" {
			title = doc.Get("title")
		}

		// Write to vault
		slug := slugify(title)
		relPath := filepath.Join("brain", "audible", slug+".md")
		vaultPath := filepath.Join(vaultDir, relPath)

		os.MkdirAll(filepath.Dir(vaultPath), 0755)

		md := fmt.Sprintf(`---
title: "%s"
source: audible
type: highlights
created: %s
---

%s
`, title, time.Now().Format("2006-01-02"), content)

		if err := os.WriteFile(vaultPath, []byte(md), 0644); err != nil {
			log.Printf("wraith: audible write error: %v", err)
			continue
		}

		state.Record("audible", externalID, "", title, relPath)
		ingested++

		if ingested >= maxItems {
			break
		}
	}

	return ingested, nil
}
