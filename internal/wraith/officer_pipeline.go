package wraith

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/GetModus/wraith/internal/markdown"
	"github.com/GetModus/wraith/internal/moduscfg"
)

// OfficerAssignments captures active staffing used for wraith handoff audit records.
type OfficerAssignments struct {
	ScoutModel     string `json:"scout_model"`
	LibrarianModel string `json:"librarian_model"`
}

// ScoutAssessment is the explicit intake handoff output from Scout.
type ScoutAssessment struct {
	Class   string `json:"class"` // keep, discard, mission_candidate
	Reason  string `json:"reason"`
	Officer string `json:"officer"`
	Model   string `json:"model"`
	At      string `json:"at"`
}

// FilingReceipt is the explicit write handoff output from Librarian.
type FilingReceipt struct {
	VaultPath string `json:"vault_path"`
	Officer   string `json:"officer"`
	Model     string `json:"model"`
	Checksum  string `json:"checksum"`
	At        string `json:"at"`
}

// OfficerHandoffRecord captures one full scout->librarian chain for a capture.
type OfficerHandoffRecord struct {
	CaptureID   string          `json:"capture_id"`
	Fingerprint string          `json:"fingerprint,omitempty"`
	Source      string          `json:"source"`
	URL         string          `json:"url,omitempty"`
	Title       string          `json:"title,omitempty"`
	Scout       ScoutAssessment `json:"scout"`
	Librarian   *FilingReceipt  `json:"librarian,omitempty"`
}

// ScoutOfficer evaluates incoming capture intent before filing.
type ScoutOfficer interface {
	Assess(c *Capture) ScoutAssessment
}

// LibrarianOfficer performs persistent write operations.
type LibrarianOfficer interface {
	File(vaultDir string, cap *Capture, frontmatter map[string]interface{}, body string) (FilingReceipt, error)
}

// OfficerPipeline defines the authorities used during queue processing.
type OfficerPipeline struct {
	Scout     ScoutOfficer
	Librarian LibrarianOfficer
}

type defaultScoutOfficer struct {
	model string
}

func (s defaultScoutOfficer) Assess(c *Capture) ScoutAssessment {
	class := "keep"
	reason := "baseline intake; retain for triage"

	lowerURL := strings.ToLower(strings.TrimSpace(c.URL))
	lowerTitle := strings.ToLower(strings.TrimSpace(c.Title))
	if strings.Contains(lowerURL, "github.com") || strings.Contains(lowerTitle, "release") || strings.Contains(lowerTitle, "cve-") {
		class = "mission_candidate"
		reason = "signal suggests actionable implementation or security follow-up"
	}
	hasTweetText := c.Tweet != nil && strings.TrimSpace(c.Tweet.Text) != ""
	if strings.TrimSpace(c.BodyText) == "" && strings.TrimSpace(c.Selected) == "" && strings.TrimSpace(c.Title) == "" && !hasTweetText {
		class = "discard"
		reason = "empty capture payload"
	}

	return ScoutAssessment{
		Class:   class,
		Reason:  reason,
		Officer: "scout",
		Model:   s.model,
		At:      time.Now().UTC().Format(time.RFC3339),
	}
}

type defaultLibrarianOfficer struct {
	model string
}

func (l defaultLibrarianOfficer) File(vaultDir string, cap *Capture, frontmatter map[string]interface{}, body string) (FilingReceipt, error) {
	dir := captureDir(vaultDir, cap)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return FilingReceipt{}, fmt.Errorf("mkdir capture dir: %w", err)
	}

	date := time.Now().Format("2006-01-02")
	slug := slugify(cap.Title)
	if slug == "" {
		slug = "capture-" + cap.ID
	}
	filename := fmt.Sprintf("%s-%s.md", date, slug)
	path := filepath.Join(dir, filename)
	if err := markdown.Write(path, frontmatter, body); err != nil {
		return FilingReceipt{}, err
	}

	sum := sha256.Sum256([]byte(body))
	return FilingReceipt{
		VaultPath: path,
		Officer:   "librarian",
		Model:     l.model,
		Checksum:  hex.EncodeToString(sum[:12]),
		At:        time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func defaultOfficerPipeline() OfficerPipeline {
	assign := loadOfficerAssignments()
	return OfficerPipeline{
		Scout:     defaultScoutOfficer{model: assign.ScoutModel},
		Librarian: defaultLibrarianOfficer{model: assign.LibrarianModel},
	}
}

func loadOfficerAssignments() OfficerAssignments {
	assign := OfficerAssignments{
		ScoutModel:     moduscfg.DefaultAssignment("scout").Model,
		LibrarianModel: moduscfg.DefaultAssignment("librarian").Model,
	}
	cfg, err := moduscfg.LoadDefault()
	if err != nil || cfg == nil {
		return assign
	}
	if strings.TrimSpace(cfg.Officers.Scout.Model) != "" {
		assign.ScoutModel = strings.TrimSpace(cfg.Officers.Scout.Model)
	}
	if strings.TrimSpace(cfg.Officers.Librarian.Model) != "" {
		assign.LibrarianModel = strings.TrimSpace(cfg.Officers.Librarian.Model)
	}
	return assign
}

func appendOfficerHandoff(dataDir string, rec OfficerHandoffRecord) error {
	if strings.TrimSpace(dataDir) == "" {
		return nil
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dataDir, "wraith-officer-handoffs.jsonl")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}
