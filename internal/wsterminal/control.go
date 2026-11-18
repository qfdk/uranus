package wsterminal

import (
	"encoding/json"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

// ControlMessage defines the structure for control messages from client
type ControlMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// ResizeMessage defines the structure for resize messages
type ResizeMessage struct {
	Rows uint16 `json:"rows"`
	Cols uint16 `json:"cols"`
}

// handleControlMessage processes control messages from the client
func handleControlMessage(t *Terminal, message []byte) {
	log.Printf("[WS Terminal] Received control message: %s", string(message))

	var controlMsg ControlMessage
	if err := json.Unmarshal(message, &controlMsg); err != nil {
		log.Printf("[WS Terminal] Failed to parse control message: %v", err)
		return
	}

	log.Printf("[WS Terminal] Processing control message of type: %s", controlMsg.Type)

	switch controlMsg.Type {
	case "resize":
		log.Printf("[WS Terminal] Processing resize command")
		var resizeData ResizeMessage

		if err := json.Unmarshal(controlMsg.Data, &resizeData); err != nil {
			log.Printf("[WS Terminal] Failed to parse resize data: %v", err)
			return
		}

		if err := t.Resize(resizeData.Rows, resizeData.Cols); err != nil {
			log.Printf("[WS Terminal] Failed to resize terminal: %v", err)
		}

	case "ping":
		response := map[string]string{
			"type": "pong",
			"data": "pong",
		}
		responseBytes, _ := json.Marshal(response)
		if err := t.write(websocket.TextMessage, responseBytes); err != nil {
			log.Printf("[WS Terminal] Failed to send pong: %v", err)
		}

	case "interrupt":
		// 等同于在终端里按 Ctrl+C：由终端驱动把 SIGINT 发给前台进程，不影响会话外的进程
		if _, err := t.shell.PTY().Write([]byte{3}); err != nil {
			log.Printf("[WS Terminal] Failed to send interrupt: %v", err)
		}

	case "terminate":
		log.Printf("[WS Terminal] Received terminate command, closing terminal")
		response := map[string]string{
			"type": "terminated",
			"data": "Terminal session closed by client",
		}
		responseBytes, _ := json.Marshal(response)
		_ = t.write(websocket.TextMessage, responseBytes)
		go func() {
			time.Sleep(100 * time.Millisecond)
			t.Close()
		}()

	default:
		log.Printf("[WS Terminal] Unknown control message type: %s", controlMsg.Type)
	}
}
