package wsterminal

import (
	"encoding/json"
	"log"
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
		
		// controlMsg.Data is already a json.RawMessage ([]byte), so we can use it directly
		if err := json.Unmarshal(controlMsg.Data, &resizeData); err != nil {
			log.Printf("[WS Terminal] Failed to parse resize data: %v, data: %s", err, string(controlMsg.Data))
			return
		}

		log.Printf("[WS Terminal] Resizing terminal: session=%s, rows=%d, cols=%d", 
			t.ID, resizeData.Rows, resizeData.Cols)
		
		if err := t.Resize(resizeData.Rows, resizeData.Cols); err != nil {
			log.Printf("[WS Terminal] Failed to resize terminal: %v", err)
		} else {
			log.Printf("[WS Terminal] Terminal resized successfully")
		}
		
	case "ping":
		log.Printf("[WS Terminal] Responding to ping with pong")
		// Send pong response to keep connection alive
		response := map[string]string{
			"type": "pong",
			"data": "pong",
		}
		responseBytes, _ := json.Marshal(response)
		err := t.WsConn.WriteMessage(1, responseBytes)
		if err != nil {
			log.Printf("[WS Terminal] Failed to send pong: %v", err)
		}
		
	case "interrupt":
		log.Printf("[WS Terminal] Received interrupt command, sending SIGINT")
		// 处理中断信号
		err := t.SendInterrupt()
		if err != nil {
			log.Printf("[WS Terminal] Failed to send interrupt: %v", err)
		}
		
	default:
		log.Printf("[WS Terminal] Unknown control message type: %s", controlMsg.Type)
	}
}