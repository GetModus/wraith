package server

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/GetModus/wraith/internal/wraith"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// WraithBridge manages the WebSocket connection to the Safari extension
// and the capture queue.
type WraithBridge struct {
	mu     sync.RWMutex
	queue  *wraith.Queue
	conn   *websocket.Conn
	status BridgeStatus
}

// BridgeStatus tracks the extension connection state.
type BridgeStatus struct {
	Connected   bool   `json:"connected"`
	ExtensionID string `json:"extension_id,omitempty"`
	UserAgent   string `json:"user_agent,omitempty"`
	ConnectedAt string `json:"connected_at,omitempty"`
	LastMessage string `json:"last_message,omitempty"`
}

// NewWraithBridge creates a bridge with the given queue.
func NewWraithBridge(queue *wraith.Queue) *WraithBridge {
	return &WraithBridge{
		queue: queue,
	}
}

// wsEnvelope is the message format between extension and server.
type wsEnvelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
	SentAt  string          `json:"sentAt,omitempty"`
	ID      string          `json:"id,omitempty"`
	Command string          `json:"command,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// wsHelloPayload is sent by the extension on connect.
type wsHelloPayload struct {
	UserAgent   string `json:"userAgent"`
	ExtensionID string `json:"extensionId"`
}

// wsCapturePayload is sent when the extension captures a page.
type wsCapturePayload struct {
	Source   string                  `json:"source"`
	URL      string                  `json:"url"`
	Title    string                  `json:"title"`
	BodyText string                  `json:"bodyText,omitempty"`
	Selected string                  `json:"selected,omitempty"`
	Author   string                  `json:"author,omitempty"`
	SiteName string                  `json:"siteName,omitempty"`
	Headings []wraith.CaptureHeading `json:"headings,omitempty"`
	Links    []wraith.CaptureLink    `json:"links,omitempty"`
	Images   []wraith.CaptureImage   `json:"images,omitempty"`
	Tweet    *wraith.CaptureTweet    `json:"tweet,omitempty"`
	Meta     map[string]interface{}  `json:"meta,omitempty"`
}

// HandleWebSocket handles the /wraith/ws endpoint.
func (b *WraithBridge) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("wraith/bridge: upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	b.mu.Lock()
	// Close existing connection if any
	if b.conn != nil {
		b.conn.Close()
	}
	b.conn = conn
	b.status.Connected = true
	b.status.ConnectedAt = time.Now().UTC().Format(time.RFC3339)
	b.mu.Unlock()

	log.Println("wraith/bridge: extension connected")

	defer func() {
		b.mu.Lock()
		b.status.Connected = false
		b.conn = nil
		b.mu.Unlock()
		log.Println("wraith/bridge: extension disconnected")
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("wraith/bridge: read error: %v", err)
			}
			return
		}

		b.mu.Lock()
		b.status.LastMessage = time.Now().UTC().Format(time.RFC3339)
		b.mu.Unlock()

		b.handleMessage(conn, message)
	}
}

func (b *WraithBridge) handleMessage(conn *websocket.Conn, raw []byte) {
	var env wsEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		log.Printf("wraith/bridge: invalid JSON: %v", err)
		return
	}

	switch env.Type {
	case "hello":
		var hello wsHelloPayload
		json.Unmarshal(env.Payload, &hello)
		b.mu.Lock()
		b.status.ExtensionID = hello.ExtensionID
		b.status.UserAgent = hello.UserAgent
		b.mu.Unlock()
		log.Printf("wraith/bridge: hello from %s", hello.ExtensionID)

		// Acknowledge
		b.sendJSON(conn, map[string]interface{}{
			"type":    "hello_ack",
			"payload": map[string]interface{}{"server": "modus", "version": "1.0"},
		})

	case "capture":
		var cap wsCapturePayload
		if err := json.Unmarshal(env.Payload, &cap); err != nil {
			log.Printf("wraith/bridge: invalid capture payload: %v", err)
			b.sendJSON(conn, map[string]interface{}{
				"type": "capture_ack",
				"payload": map[string]interface{}{
					"ok":    false,
					"error": "invalid payload",
				},
			})
			return
		}

		capture := &wraith.Capture{
			Source:   cap.Source,
			URL:      cap.URL,
			Title:    cap.Title,
			BodyText: cap.BodyText,
			Selected: cap.Selected,
			Author:   cap.Author,
			SiteName: cap.SiteName,
			Headings: cap.Headings,
			Links:    cap.Links,
			Images:   cap.Images,
			Tweet:    cap.Tweet,
			Meta:     cap.Meta,
		}

		if err := b.queue.Enqueue(capture); err != nil {
			log.Printf("wraith/bridge: enqueue failed: %v", err)
			b.sendJSON(conn, map[string]interface{}{
				"type": "capture_ack",
				"payload": map[string]interface{}{
					"ok":    false,
					"error": err.Error(),
				},
			})
			return
		}

		b.sendJSON(conn, map[string]interface{}{
			"type": "capture_ack",
			"payload": map[string]interface{}{
				"ok": true,
				"id": capture.ID,
			},
		})

	case "bookmark_batch":
		var batch struct {
			Items []wsCapturePayload `json:"items"`
		}
		if err := json.Unmarshal(env.Payload, &batch); err != nil {
			log.Printf("wraith/bridge: invalid batch payload: %v", err)
			return
		}

		count := 0
		for _, cap := range batch.Items {
			capture := &wraith.Capture{
				Source:   "bookmark-sync",
				URL:      cap.URL,
				Title:    cap.Title,
				BodyText: cap.BodyText,
				Author:   cap.Author,
				Tweet:    cap.Tweet,
				Meta:     cap.Meta,
			}
			if err := b.queue.Enqueue(capture); err != nil {
				log.Printf("wraith/bridge: batch enqueue error: %v", err)
				continue
			}
			count++
		}

		b.sendJSON(conn, map[string]interface{}{
			"type": "batch_ack",
			"payload": map[string]interface{}{
				"ok":       true,
				"enqueued": count,
				"total":    len(batch.Items),
			},
		})

	case "status":
		// Extension reporting its own status — log and acknowledge
		log.Printf("wraith/bridge: extension status update")

	case "command_result":
		// Response to a command we sent — log for now
		log.Printf("wraith/bridge: command result received")

	default:
		log.Printf("wraith/bridge: unknown message type: %s", env.Type)
	}
}

// SendCommand sends a command to the connected extension.
func (b *WraithBridge) SendCommand(command string, params interface{}) error {
	b.mu.RLock()
	conn := b.conn
	b.mu.RUnlock()

	if conn == nil {
		return &BridgeError{msg: "extension not connected"}
	}

	paramsJSON, _ := json.Marshal(params)
	return b.sendJSON(conn, map[string]interface{}{
		"id":      generateCommandID(),
		"command": command,
		"params":  json.RawMessage(paramsJSON),
	})
}

// GetStatus returns the current bridge status.
func (b *WraithBridge) GetStatus() BridgeStatus {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.status
}

func (b *WraithBridge) sendJSON(conn *websocket.Conn, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}

func generateCommandID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return "cmd-" + hex.EncodeToString(b)
}

// BridgeError indicates the bridge is not connected.
type BridgeError struct {
	msg string
}

func (e *BridgeError) Error() string { return e.msg }

type handoffStats struct {
	Path          string         `json:"path"`
	Available     bool           `json:"available"`
	Total         int            `json:"total"`
	WithLibrarian int            `json:"with_librarian"`
	ByClass       map[string]int `json:"by_class,omitempty"`
	LastCaptureID string         `json:"last_capture_id,omitempty"`
	LastAt        string         `json:"last_at,omitempty"`
	ParseErrors   int            `json:"parse_errors,omitempty"`
}

type handoffLine struct {
	CaptureID string `json:"capture_id"`
	Scout     struct {
		Class string `json:"class"`
		At    string `json:"at"`
	} `json:"scout"`
	Librarian *struct {
		At string `json:"at"`
	} `json:"librarian,omitempty"`
}

func handoffLogPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "modus", "data", "wraith-officer-handoffs.jsonl"), nil
}

func loadHandoffStats() (handoffStats, error) {
	path, err := handoffLogPath()
	if err != nil {
		return handoffStats{}, err
	}
	out := handoffStats{
		Path:      path,
		ByClass:   map[string]int{},
		Available: false,
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, fmt.Errorf("open handoff log: %w", err)
	}
	defer f.Close()
	out.Available = true

	sc := bufio.NewScanner(f)
	const maxLine = 1 << 20
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, maxLine)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var h handoffLine
		if err := json.Unmarshal([]byte(line), &h); err != nil {
			out.ParseErrors++
			continue
		}
		out.Total++
		if h.Librarian != nil {
			out.WithLibrarian++
		}
		class := strings.TrimSpace(strings.ToLower(h.Scout.Class))
		if class != "" {
			out.ByClass[class]++
		}
		out.LastCaptureID = h.CaptureID
		if h.Librarian != nil && h.Librarian.At != "" {
			out.LastAt = h.Librarian.At
		} else {
			out.LastAt = h.Scout.At
		}
	}
	if err := sc.Err(); err != nil {
		return out, fmt.Errorf("scan handoff log: %w", err)
	}
	return out, nil
}
