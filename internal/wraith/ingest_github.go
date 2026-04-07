package wraith

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const githubAPI = "https://api.github.com"
const githubPerPage = 100

type ghStarEntry struct {
	StarredAt string `json:"starred_at"`
	Repo      ghRepo `json:"repo"`
}

type ghRepo struct {
	FullName    string   `json:"full_name"`
	HTMLURL     string   `json:"html_url"`
	Description string   `json:"description"`
	Language    string   `json:"language"`
	Stars       int      `json:"stargazers_count"`
	Topics      []string `json:"topics"`
	Fork        bool     `json:"fork"`
	Homepage    string   `json:"homepage"`
	License     *struct {
		SPDXID string `json:"spdx_id"`
	} `json:"license"`
	Owner struct {
		Login string `json:"login"`
	} `json:"owner"`
}

// IngestGitHub fetches starred repos for a GitHub user and writes them to vault.
func IngestGitHub(vaultDir string, state *State, username string, maxItems int) (int, error) {
	token := os.Getenv("GITHUB_TOKEN")

	client := &http.Client{Timeout: 30 * time.Second}
	ingested := 0
	page := 1

	for ingested < maxItems {
		req, _ := http.NewRequest("GET", fmt.Sprintf("%s/users/%s/starred?per_page=%d&page=%d", githubAPI, username, githubPerPage, page), nil)
		req.Header.Set("Accept", "application/vnd.github.v3.star+json")
		req.Header.Set("User-Agent", "MODUS-Enclave/1.0")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := client.Do(req)
		if err != nil {
			return ingested, fmt.Errorf("github fetch: %w", err)
		}

		if resp.StatusCode == 403 {
			resp.Body.Close()
			retryAfter := resp.Header.Get("Retry-After")
			if retryAfter != "" {
				log.Printf("wraith: GitHub rate limited, retry-after: %s", retryAfter)
			}
			return ingested, fmt.Errorf("github rate limited")
		}

		if resp.StatusCode != 200 {
			resp.Body.Close()
			return ingested, fmt.Errorf("github status %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var entries []ghStarEntry
		if err := json.Unmarshal(body, &entries); err != nil {
			return ingested, fmt.Errorf("github parse: %w", err)
		}

		if len(entries) == 0 {
			break
		}

		for _, entry := range entries {
			repo := entry.Repo
			externalID := "gh:" + repo.FullName

			if state.Exists("github", externalID) {
				continue
			}

			// Build body
			var parts []string
			if repo.Description != "" {
				parts = append(parts, repo.Description)
			}
			if repo.Language != "" {
				parts = append(parts, fmt.Sprintf("Language: %s", repo.Language))
			}
			parts = append(parts, fmt.Sprintf("Stars: %d", repo.Stars))
			if len(repo.Topics) > 0 {
				parts = append(parts, fmt.Sprintf("Topics: %s", strings.Join(repo.Topics, ", ")))
			}
			if entry.StarredAt != "" {
				parts = append(parts, fmt.Sprintf("Starred: %s", entry.StarredAt))
			}
			if repo.Homepage != "" {
				parts = append(parts, fmt.Sprintf("Homepage: %s", repo.Homepage))
			}
			if repo.Fork {
				parts = append(parts, "(Fork)")
			}
			if repo.License != nil && repo.License.SPDXID != "" {
				parts = append(parts, fmt.Sprintf("License: %s", repo.License.SPDXID))
			}
			mdBody := strings.Join(parts, "\n")

			// Build tags
			var tags []string
			if repo.Language != "" {
				tags = append(tags, "lang:"+strings.ToLower(repo.Language))
			}
			for _, t := range repo.Topics {
				if len(tags) < 10 {
					tags = append(tags, t)
				}
			}

			// Write to vault
			slug := slugify(repo.FullName)
			relPath := filepath.Join("brain", "github", slug+".md")
			fullPath := filepath.Join(vaultDir, relPath)

			os.MkdirAll(filepath.Dir(fullPath), 0755)

			content := fmt.Sprintf(`---
title: "%s"
source: github
url: %s
author: %s
tags: [%s]
starred: %s
created: %s
---

%s
`, repo.FullName, repo.HTMLURL, repo.Owner.Login,
				strings.Join(tags, ", "), entry.StarredAt,
				time.Now().Format("2006-01-02"), mdBody)

			if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
				log.Printf("wraith: github write error: %v", err)
				continue
			}

			state.Record("github", externalID, repo.HTMLURL, repo.FullName, relPath)
			ingested++

			if ingested >= maxItems {
				break
			}
		}

		if len(entries) < githubPerPage {
			break
		}
		page++
	}

	return ingested, nil
}
