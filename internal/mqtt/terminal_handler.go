// internal/mqtt/terminal_handler.go
package mqtt

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"uranus/internal/mqtty"
)

// 终端相关MQTT消息处理

var (
	// 会话管理互斥锁
	terminalSessionsMutex sync.RWMutex

	// 会话管理映射表
	terminalSessions = make(map[string]*mqtty.Session)

	// MQTTY会话管理器
	sessionManager *mqtty.SessionManager
)

// 初始化终端会话管理器
func InitTerminalSessionManager() {
	sessionManager = mqtty.NewSessionManager()
}

// 处理终端命令
func HandleTerminalCommand(clientID string, requestID string, agentUUID string, payload []byte) {
	log.Printf("[MQTT][HandleTerminalCommand] 收到终端命令: %s", string(payload))

	// 解析命令数据
	var cmdData map[string]interface{}
	if err := json.Unmarshal(payload, &cmdData); err != nil {
		log.Printf("[MQTT] 解析终端命令数据失败: %v", err)
		SendErrorResponse(agentUUID, requestID, "解析终端命令数据失败", err)
		return
	}

	// 提取命令类型
	cmdType, ok := cmdData["type"].(string)
	if !ok || cmdType == "" {
		// 如果类型为空，默认使用create类型
		cmdType = "create"
		log.Printf("[MQTT] 命令类型为空，使用默认类型: %s", cmdType)
	}

	// 会话ID是必需的
	sessionID, ok := cmdData["sessionId"].(string)
	if !ok || sessionID == "" {
		log.Printf("[MQTT] 缺少会话ID")
		SendErrorResponse(agentUUID, requestID, "缺少会话ID", nil)
		return
	}

	// 根据命令类型处理
	switch cmdType {
	case "create":
		// 创建新终端会话
		handleCreateTerminal(agentUUID, requestID, sessionID)

	case "close":
		// 关闭终端会话
		handleCloseTerminal(agentUUID, requestID, sessionID)

	case "input":
		// 处理终端输入
		rawInput, _ := cmdData["data"].(string)
		handleTerminalInput(agentUUID, requestID, sessionID, rawInput)

	case "resize":
		// 处理终端大小调整
		resizeData, ok := cmdData["data"].(map[string]interface{})
		if !ok {
			log.Printf("[MQTT] 无效的调整大小数据")
			SendErrorResponse(agentUUID, requestID, "无效的调整大小数据", nil)
			return
		}
		rows, _ := resizeData["rows"].(float64)
		cols, _ := resizeData["cols"].(float64)
		handleTerminalResize(agentUUID, requestID, sessionID, uint16(rows), uint16(cols))

	default:
		log.Printf("[MQTT] 未知的终端命令类型: %s", cmdType)
		SendErrorResponse(agentUUID, requestID, fmt.Sprintf("未知的终端命令类型: %s", cmdType), nil)
	}
}

// 处理创建终端会话
func handleCreateTerminal(agentUUID string, requestID string, sessionID string) {
	log.Printf("[MQTT] 创建终端会话: %s", sessionID)

	// 检查会话是否已存在
	terminalSessionsMutex.RLock()
	_, exists := terminalSessions[sessionID]
	terminalSessionsMutex.RUnlock()

	if exists {
		log.Printf("[MQTT] 会话ID已存在: %s", sessionID)
		SendErrorResponse(agentUUID, requestID, "会话ID已存在", nil)
		return
	}

	// 使用会话管理器创建新会话
	if err := sessionManager.CreateSession(sessionID, ""); err != nil {
		log.Printf("[MQTT] 创建终端会话失败: %v", err)
		SendErrorResponse(agentUUID, requestID, "创建终端会话失败", err)
		return
	}

	// 获取创建的会话
	session, err := sessionManager.GetSession(sessionID)
	if err != nil {
		log.Printf("[MQTT] 获取创建的会话失败: %v", err)
		SendErrorResponse(agentUUID, requestID, "获取创建的会话失败", err)
		return
	}

	// 保存会话到映射表
	terminalSessionsMutex.Lock()
	terminalSessions[sessionID] = session
	terminalSessionsMutex.Unlock()

	// 启动输出处理
	go handleTerminalOutput(agentUUID, sessionID, session)

	// 发送成功响应
	response := ResponseMessage{
		Command:   "terminal",
		RequestID: requestID,
		Success:   true,
		Message:   "终端会话已创建",
		SessionID: sessionID,
	}

	SendResponse(&response)

	// 发送创建成功的状态消息
	SendStatusResponse(agentUUID, sessionID, "created", nil)
}

// 处理关闭终端会话
func handleCloseTerminal(agentUUID string, requestID string, sessionID string) {
	log.Printf("[MQTT] 关闭终端会话: %s", sessionID)

	// 检查会话是否存在
	terminalSessionsMutex.RLock()
	_, exists := terminalSessions[sessionID]
	terminalSessionsMutex.RUnlock()

	if !exists {
		log.Printf("[MQTT] 会话不存在: %s", sessionID)
		SendErrorResponse(agentUUID, requestID, "会话不存在", nil)
		return
	}

	// 关闭会话
	err := sessionManager.CloseSession(sessionID)

	// 从映射表中删除会话
	terminalSessionsMutex.Lock()
	delete(terminalSessions, sessionID)
	terminalSessionsMutex.Unlock()

	if err != nil {
		log.Printf("[MQTT] 关闭终端会话失败: %v", err)
		SendErrorResponse(agentUUID, requestID, "关闭终端会话失败", err)
		return
	}

	// 发送成功响应
	response := ResponseMessage{
		Command:   "terminal",
		RequestID: requestID,
		Success:   true,
		Message:   "终端会话已关闭",
		SessionID: sessionID,
	}

	SendResponse(&response)

	// 发送状态更新
	SendStatusResponse(agentUUID, sessionID, "closed", nil)
}

// 处理终端输入
func handleTerminalInput(agentUUID string, requestID string, sessionID string, input string) {
	// 检查会话是否存在
	terminalSessionsMutex.RLock()
	session, exists := terminalSessions[sessionID]
	terminalSessionsMutex.RUnlock()

	if !exists {
		log.Printf("[MQTT] 会话不存在: %s", sessionID)
		SendErrorResponse(agentUUID, requestID, "会话不存在", nil)
		return
	}

	// 发送输入到会话
	if err := session.SendInput([]byte(input)); err != nil {
		log.Printf("[MQTT] 发送输入到会话失败: %v", err)
		SendErrorResponse(agentUUID, requestID, "发送输入到会话失败", err)
		return
	}

	// 发送成功响应
	response := ResponseMessage{
		Command:   "terminal",
		RequestID: requestID,
		Success:   true,
		Message:   "输入已处理",
		SessionID: sessionID,
	}

	SendResponse(&response)
}

// 处理终端大小调整
func handleTerminalResize(agentUUID string, requestID string, sessionID string, rows uint16, cols uint16) {
	// 检查会话是否存在
	terminalSessionsMutex.RLock()
	session, exists := terminalSessions[sessionID]
	terminalSessionsMutex.RUnlock()

	if !exists {
		log.Printf("[MQTT] 会话不存在: %s", sessionID)
		SendErrorResponse(agentUUID, requestID, "会话不存在", nil)
		return
	}

	// 调整终端大小
	if err := session.Resize(rows, cols); err != nil {
		log.Printf("[MQTT] 调整终端大小失败: %v", err)
		SendErrorResponse(agentUUID, requestID, "调整终端大小失败", err)
		return
	}

	// 发送成功响应
	response := ResponseMessage{
		Command:   "terminal",
		RequestID: requestID,
		Success:   true,
		Message:   "终端大小已调整",
		SessionID: sessionID,
	}

	SendResponse(&response)
}

// 处理终端输出
func handleTerminalOutput(agentUUID string, sessionID string, session *mqtty.Session) {
	log.Printf("[MQTT] 启动终端输出处理: %s", sessionID)

	// 循环读取终端输出
	for output := range session.Output {
		// 创建输出消息
		outputMsg := map[string]interface{}{
			"sessionId": sessionID,
			"type":      "output",
			"data":      string(output),
			"timestamp": GetTimestamp(),
		}

		// 序列化消息
		outputBytes, err := json.Marshal(outputMsg)
		if err != nil {
			log.Printf("[MQTT] 序列化输出消息失败: %v", err)
			continue
		}

		// 发布输出消息
		PublishToResponseTopic(agentUUID, outputBytes)
	}

	log.Printf("[MQTT] 终端输出处理结束: %s", sessionID)
}
