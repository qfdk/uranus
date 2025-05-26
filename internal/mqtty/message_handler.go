package mqtty

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"sync"
	"sync/atomic"
	"time"
	"uranus/internal/config"
	"uranus/internal/services"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// max 返回两个整数中的最大值
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// 缓存响应主题以避免重复格式化
var responseTopicCache = make(map[string]string)
var responseTopicMutex sync.RWMutex

// 使用已发送消息缓存，防止重复消息
var (
	recentOutputs      = make(map[string]time.Time)
	recentOutputsMutex sync.RWMutex
	maxRecentOutputs   = 100 // 最多缓存100条最近消息
)

// 输出缓冲设置
const (
	// 输出缓冲区大小
	OutputBufferSize        = 32768 // 32 KB
	OutputAccumulationDelay = 1 * time.Millisecond
)

// 缓存UUID以减少频繁访问AppConfig
var cachedUUID atomic.Value
var uuidInitOnce sync.Once

// getUUID 获取缓存的UUID
func getUUID() string {
	uuidInitOnce.Do(func() {
		cachedUUID.Store(config.GetAppConfig().UUID)
	})
	return cachedUUID.Load().(string)
}

// getResponseTopic 获取响应主题，使用缓存
func getResponseTopic(agentUuid string) string {
	responseTopicMutex.RLock()
	topic, exists := responseTopicCache[agentUuid]
	responseTopicMutex.RUnlock()

	if !exists {
		topic = fmt.Sprintf("uranus/response/%s", agentUuid)
		responseTopicMutex.Lock()
		responseTopicCache[agentUuid] = topic
		responseTopicMutex.Unlock()
	}

	return topic
}

// 处理从命令主题接收到的消息
func handleCommandMessage(client mqtt.Client, msg mqtt.Message, topicPrefix string, manager *SessionManager, agentUuid string) {
	// 解析消息内容
	var command struct {
		Command   string      `json:"command"`
		RequestId string      `json:"requestId"`
		ClientId  string      `json:"clientId"`
		Type      string      `json:"type"`
		SessionId string      `json:"sessionId"`
		Data      interface{} `json:"data"`
	}

	if err := json.Unmarshal(msg.Payload(), &command); err != nil {
		log.Printf("[MQTTY] 解析命令消息失败: %v", err)
		return
	}

	log.Printf("[MQTTY] 收到命令: %s", msg.Payload())

	// 终端命令另行处理
	if command.Command == "terminal" {
		handleTerminalCommand(client, command, manager, agentUuid, topicPrefix)
		return
	}

	// 处理Nginx相关命令
	switch command.Command {
	case "reload":
		log.Printf("[MQTTY] 收到Nginx重载命令")
		handleReloadCommand(client, command, agentUuid)
		return
	case "start":
		log.Printf("[MQTTY] 收到Nginx启动命令")
		handleStartCommand(client, command, agentUuid)
		return
	case "stop":
		log.Printf("[MQTTY] 收到Nginx停止命令")
		handleStopCommand(client, command, agentUuid)
		return
	case "restart":
		log.Printf("[MQTTY] 收到Nginx重启命令")
		handleRestartCommand(client, command, agentUuid)
		return
	}

	// 其他命令类型的处理继续原来的逻辑
	// 转换为正确的消息格式
	message := Message{
		SessionID: command.SessionId,
		Type:      command.Type,
		Data:      command.Data,
		Timestamp: time.Now().UnixNano() / 1e6,
		RequestId: command.RequestId,
	}

	// 根据命令类型处理
	switch command.Type {
	case "create":
		handleControlMessage(command.SessionId, &message, manager, topicPrefix)

		// 发送带请求ID的成功响应
		response := struct {
			Success   bool   `json:"success"`
			RequestId string `json:"requestId"`
			SessionId string `json:"sessionId"`
			Type      string `json:"type"`
			Message   string `json:"message,omitempty"`
		}{
			Success:   true,
			RequestId: command.RequestId,
			SessionId: command.SessionId,
			Type:      "created",
			Message:   "终端会话已创建",
		}

		// 发送响应
		responseTopic := fmt.Sprintf("uranus/response/%s", agentUuid)
		respPayload, _ := json.Marshal(response)
		client.Publish(responseTopic, 1, false, respPayload)

	case "input":
		handleInputMessage(command.SessionId, &message, manager)

	case "resize":
		handleResizeMessage(command.SessionId, &message, manager)

	case "close":
		handleControlMessage(command.SessionId, &message, manager, topicPrefix)

		// 发送带请求ID的成功响应
		response := struct {
			Success   bool   `json:"success"`
			RequestId string `json:"requestId"`
			SessionId string `json:"sessionId"`
			Type      string `json:"type"`
			Message   string `json:"message,omitempty"`
		}{
			Success:   true,
			RequestId: command.RequestId,
			SessionId: command.SessionId,
			Type:      "closed",
			Message:   "终端会话已关闭",
		}

		// 发送响应
		responseTopic := fmt.Sprintf("uranus/response/%s", agentUuid)
		respPayload, _ := json.Marshal(response)
		client.Publish(responseTopic, 1, false, respPayload)
	}
}

// 处理接收到的消息
func handleMessage(client mqtt.Client, msg mqtt.Message, topicPrefix string, manager *SessionManager) {
	topic := msg.Topic()
	_, msgType, ok := parseTopicParts(topic, topicPrefix)
	if !ok {
		log.Printf("[MQTTY] 无法解析主题: %s", topic)
		return
	}

	var message Message
	if err := json.Unmarshal(msg.Payload(), &message); err != nil {
		log.Printf("[MQTTY] 解析消息失败: %v", err)
		return
	}

	// 从消息中获取sessionID
	sessionID := message.SessionID

	switch msgType {
	case TopicInput:
		handleInputMessage(sessionID, &message, manager)
	case TopicControl:
		handleControlMessage(sessionID, &message, manager, topicPrefix)
	case TopicResize:
		handleResizeMessage(sessionID, &message, manager)
	}
}

// 处理输入消息
func handleInputMessage(sessionID string, msg *Message, manager *SessionManager) {
	session, err := manager.GetSession(sessionID)
	if err != nil {
		log.Printf("[MQTTY] 会话不存在: %s", sessionID)
		return
	}

	// 检查会话是否关闭
	if isSessionClosed(session) {
		log.Printf("[MQTTY] 会话已关闭，无法发送输入: %s", sessionID)
		return
	}

	// 转换数据
	input := extractInputData(msg.Data)
	if input == "" {
		return // 错误已记录在extractInputData中
	}

	// 发送到会话
	if err := session.SendInput([]byte(input)); err != nil {
		log.Printf("[MQTTY] 发送输入失败: %v", err)
	}
}

// 提取输入数据的辅助函数
func extractInputData(data interface{}) string {
	switch v := data.(type) {
	case string:
		return v
	case map[string]interface{}:
		// 处理可能是map形式的data字段
		if data, ok := v["data"].(string); ok {
			return data
		}
		log.Printf("[MQTTY] 找不到有效的输入数据: %+v", v)
	}
	log.Printf("[MQTTY] 无效的输入格式: %T", data)
	return ""
}

// 处理控制消息
func handleControlMessage(sessionID string, msg *Message, manager *SessionManager, topicPrefix string) {
	// Type字段是字符串，直接使用
	msgType := msg.Type

	switch msgType {
	case "create":
		// 获取要使用的shell
		var shell string
		if shellData, ok := msg.Data.(string); ok && shellData != "" {
			shell = shellData
		} else {
			shell = getDefaultShell()
		}

		// 检查会话是否已经存在并且活跃
		session, err := manager.GetSession(sessionID)
		if err == nil && session != nil && !isSessionClosed(session) {
			// 会话存在且活跃，直接复用
			log.Printf("[MQTTY] 复用已存在的活跃会话: %s", sessionID)

			// 发送创建状态
			publishStatus("created")

			// 确保输出转发正在运行
			go forwardSessionOutput(topicPrefix, sessionID, manager)
			return
		}

		// 创建会话（如果不存在或已关闭）
		err = manager.CreateSession(sessionID, shell)
		if err != nil {
			log.Printf("[MQTTY] 创建会话失败: %v", err)
			// 发送错误状态
			publishStatus("error")
		} else {
			// 发送创建成功状态
			publishStatus("created")
			// 启动输出转发
			go forwardSessionOutput(topicPrefix, sessionID, manager)
		}

	case "close":
		// 关闭会话
		err := manager.CloseSession(sessionID)
		if err != nil {
			log.Printf("[MQTTY] 关闭会话失败: %v", err)
			// 发送错误状态
			publishStatus("error")
		} else {
			// 发送关闭状态
			publishStatus("closed")
		}
	}
}

// 处理调整大小消息
func handleResizeMessage(sessionID string, msg *Message, manager *SessionManager) {
	session, err := manager.GetSession(sessionID)
	if err != nil {
		log.Printf("[MQTTY] 会话不存在: %s", sessionID)
		return
	}

	// 解析尺寸数据
	var resizeData map[string]interface{}
	var ok bool

	// 处理不同的数据结构
	switch v := msg.Data.(type) {
	case map[string]interface{}:
		// 检查是否包含了嵌套的data字段
		if nestedData, hasNestedData := v["data"].(map[string]interface{}); hasNestedData {
			resizeData = nestedData
		} else {
			// 使用直接的结构
			resizeData = v
		}
		ok = true
	default:
		log.Printf("[MQTTY] 无效的调整大小数据: %T", msg.Data)
		return
	}

	if !ok || resizeData == nil {
		log.Printf("[MQTTY] 无法解析终端大小数据: %v", msg.Data)
		return
	}

	// 获取行列数据
	var rows, cols float64
	var rowsOK, colsOK bool

	rows, rowsOK = resizeData["rows"].(float64)
	cols, colsOK = resizeData["cols"].(float64)

	if !rowsOK || !colsOK {
		log.Printf("[MQTTY] 缺少行列数据: rows=%v, cols=%v", resizeData["rows"], resizeData["cols"])
		return
	}

	log.Printf("[MQTTY] 调整终端大小: 会话=%s, 行=%d, 列=%d", sessionID, int(rows), int(cols))
	if err := session.Resize(uint16(rows), uint16(cols)); err != nil {
		log.Printf("[MQTTY] 调整终端大小失败: %v", err)
	}
}

// 处理终端相关命令
func handleTerminalCommand(client mqtt.Client, command struct {
	Command   string      `json:"command"`
	RequestId string      `json:"requestId"`
	ClientId  string      `json:"clientId"`
	Type      string      `json:"type"`
	SessionId string      `json:"sessionId"`
	Data      interface{} `json:"data"`
}, manager *SessionManager, agentUuid string, topicPrefix string) {
	log.Printf("[MQTTY] 处理终端命令: %s, 会话ID: %s", command.Type, command.SessionId)

	// 创建响应主题
	responseTopic := fmt.Sprintf("uranus/response/%s", agentUuid)

	// 转换为标准消息格式
	message := Message{
		SessionID: command.SessionId,
		Type:      command.Type,
		Data:      command.Data,
		Timestamp: time.Now().UnixNano() / 1e6,
		RequestId: command.RequestId,
	}

	// 根据命令类型处理
	switch command.Type {
	case "create":
		// 创建终端会话
		log.Printf("[MQTTY] 创建终端会话: %s", command.SessionId)

		// 检查会话是否已经存在并活跃
		session, err := manager.GetSession(command.SessionId)
		if err == nil && session != nil && !isSessionClosed(session) {
			// 会话存在且活跃，直接复用
			log.Printf("[MQTTY] 复用已存在的活跃会话: %s", command.SessionId)

			// 准备成功响应
			response := struct {
				Success   bool   `json:"success"`
				RequestId string `json:"requestId"`
				SessionId string `json:"sessionId"`
				Type      string `json:"type"`
				Message   string `json:"message,omitempty"`
			}{
				Success:   true,
				RequestId: command.RequestId,
				SessionId: command.SessionId,
				Type:      "created",
				Message:   "终端会话已创建(复用现有会话)",
			}

			// 发送响应
			respPayload, _ := json.Marshal(response)
			client.Publish(responseTopic, 1, false, respPayload)

			// 确保输出转发正在运行
			go forwardSessionOutputWithUUID(topicPrefix, command.SessionId, manager, agentUuid)
			return
		}

		// 获取Shell命令（如果有）
		var shell string
		if shellData, ok := command.Data.(string); ok && shellData != "" {
			shell = shellData
		} else {
			shell = getDefaultShell()
		}

		// 创建会话
		err = manager.CreateSession(command.SessionId, shell)

		// 准备响应
		response := struct {
			Success   bool   `json:"success"`
			RequestId string `json:"requestId"`
			SessionId string `json:"sessionId"`
			Type      string `json:"type"`
			Message   string `json:"message,omitempty"`
		}{
			Success:   err == nil,
			RequestId: command.RequestId,
			SessionId: command.SessionId,
			Type:      "created",
			Message:   "终端会话已创建",
		}

		// 如果创建失败，更新消息
		if err != nil {
			response.Success = false
			response.Type = "error"
			response.Message = fmt.Sprintf("创建终端会话失败: %v", err)
			log.Printf("[MQTTY] 创建终端会话失败: %v", err)
		} else {
			// 会话创建成功后启动输出转发
			go forwardSessionOutputWithUUID(topicPrefix, command.SessionId, manager, agentUuid)
		}

		// 发送响应
		respPayload, _ := json.Marshal(response)
		client.Publish(responseTopic, 1, false, respPayload)

	case "input":
		// 检查会话是否存在
		_, err := manager.GetSession(command.SessionId)
		if err != nil {
			// 会话不存在，返回错误响应
			log.Printf("[MQTTY] 输入命令失败，会话不存在: %s", command.SessionId)
			response := struct {
				Success   bool   `json:"success"`
				RequestId string `json:"requestId"`
				SessionId string `json:"sessionId"`
				Type      string `json:"type"`
				Message   string `json:"message,omitempty"`
			}{
				Success:   false,
				RequestId: command.RequestId,
				SessionId: command.SessionId,
				Type:      "error",
				Message:   "会话ID不存在",
			}

			respPayload, _ := json.Marshal(response)
			client.Publish(responseTopic, 1, false, respPayload)
			return
		}

		// 会话存在，处理输入
		handleInputMessage(command.SessionId, &message, manager)

		// 不需要发送特定响应，输出将通过通道发送

	case "resize":
		// 处理终端调整大小
		handleResizeMessage(command.SessionId, &message, manager)

	case "close":
		// 关闭终端会话
		log.Printf("[MQTTY] 关闭终端会话: %s", command.SessionId)

		// 关闭会话
		err := manager.CloseSession(command.SessionId)

		// 准备响应
		response := struct {
			Success   bool   `json:"success"`
			RequestId string `json:"requestId"`
			SessionId string `json:"sessionId"`
			Type      string `json:"type"`
			Message   string `json:"message,omitempty"`
		}{
			Success:   err == nil,
			RequestId: command.RequestId,
			SessionId: command.SessionId,
			Type:      "closed",
			Message:   "终端会话已关闭",
		}

		// 如果关闭失败，更新消息
		if err != nil {
			response.Success = false
			response.Type = "error"
			response.Message = fmt.Sprintf("关闭终端会话失败: %v", err)
			log.Printf("[MQTTY] 关闭终端会话失败: %v", err)
		}

		// 发送响应
		respPayload, _ := json.Marshal(response)
		client.Publish(responseTopic, 1, false, respPayload)
	}
}

// 转发会话输出 - 兼容旧版本
// 使用配置文件中的 UUID
func forwardSessionOutput(topicPrefix, sessionID string, manager *SessionManager) {
	// 直接从缓存中获取UUID而不是每次都重新读取配置
	agentUuid := getUUID()
	// 调用新版本的函数
	forwardSessionOutputWithUUID(topicPrefix, sessionID, manager, agentUuid)
}

// forwardSessionOutputWithUUID 转发会话输出到指定的代理UUID
func forwardSessionOutputWithUUID(topicPrefix, sessionID string, manager *SessionManager, agentUuid string) {
	// 获取Agent UUID用于前端响应主题（使用缓存）
	responseTopic := getResponseTopic(agentUuid)

	// 检查是否已经有转发进程在运行
	forwardingMutex.Lock()
	if stopCh, exists := forwardingSessions[sessionID]; exists {
		// 通知现有转发进程停止
		close(stopCh)
		delete(forwardingSessions, sessionID)
	}

	// 创建新的停止通道
	stopCh := make(chan struct{})
	forwardingSessions[sessionID] = stopCh
	forwardingMutex.Unlock()

	// 在函数结束时清除通道
	defer func() {
		forwardingMutex.Lock()
		delete(forwardingSessions, sessionID)
		forwardingMutex.Unlock()
	}()

	session, err := manager.GetSession(sessionID)
	if err != nil {
		log.Printf("[MQTTY] 无法获取会话来转发输出: %v", err)
		return
	}

	// 打印输出主题
	log.Printf("[MQTTY] 转发输出到主题: %s", responseTopic)

	// 清理过期的输出缓存
	cleanupRecentOutputs()

	// 预先分配缓冲区以减少内存分配
	buffer := new(bytes.Buffer)

	// 存储累积的输出
	accumulated := new(bytes.Buffer)
	// 累积输出的定时器
	var flushTimer *time.Timer = nil
	// 上次发送时间
	lastSendTime := time.Now()

	// 发送函数，封装重复逻辑
	sendAccumulatedOutput := func() {
		if accumulated.Len() == 0 {
			return
		}

		// 使用时间戳作为唯一标识
		timestampNow := time.Now().UnixNano() / 1e6
		outputHash := fmt.Sprintf("%s-%d", sessionID, timestampNow)

		// 获取积累的输出
		accumulatedOutput := accumulated.String()
		// 清空积累缓冲区以备下次使用
		accumulated.Reset()

		// 打印输出的前50个字符以便于调试
		preview := accumulatedOutput
		if len(preview) > 50 {
			preview = preview[:50] + "..."
		}

		// 创建标准MQTT消息
		message := Message{
			SessionID: sessionID,
			Type:      "output", // 添加类型信息，与前端对应
			Data:      accumulatedOutput,
			Timestamp: timestampNow,
		}

		// 重用缓冲区以减少内存分配
		buffer.Reset()
		encoder := json.NewEncoder(buffer)
		if err := encoder.Encode(message); err != nil {
			log.Printf("[MQTTY] 序列化输出消息失败: %v", err)
			return
		}

		// 添加到最近消息缓存
		recentOutputsMutex.Lock()
		recentOutputs[outputHash] = time.Now()
		recentOutputsMutex.Unlock()

		// 发布到前端响应主题
		token := mqttClient.Publish(responseTopic, 1, false, buffer.Bytes())
		if token.Wait() && token.Error() != nil {
			log.Printf("[MQTTY] 发布输出消息失败: %v", token.Error())
		} else {
			log.Printf("[MQTTY] 发送会话输出 (%s): %q", sessionID, preview)
		}

		lastSendTime = time.Now()
	}

	// 检查客户端连接状态的快速路径
	var clientConnected bool
	// 最大输出积累大小，超过此大小立即发送
	const maxAccumulationSize = OutputBufferSize
	// 最大等待时间，即使积累很少也会发送
	const maxAccumulationDelay = OutputAccumulationDelay
	// 命令结束标记，用于判断是否一个命令执行完成
	commandEndMarkers := [][]byte{[]byte("$ "), []byte("# "), []byte("> ")}

	// 创建定时器但不启动
	flushTimer = time.NewTimer(maxAccumulationDelay)
	// 立即停止以便后续使用
	if !flushTimer.Stop() {
		<-flushTimer.C
	}

	for {
		// 先检查客户端连接状态
		clientConnected = mqttClient != nil && mqttClient.IsConnected()

		select {
		case output, ok := <-session.Output:
			if !ok {
				// 通道已关闭
				sendAccumulatedOutput() // 发送剩余数据
				return
			}

			// 快速路径：如果客户端未连接，跳过处理
			if !clientConnected {
				continue
			}

			// 积累输出
			accumulated.Write(output)

			// 检查是否为命令结束的标记
			isCommandEnd := false
			// 检查是否为快速命令的结果（如ls命令）- 特征是有多行输出且以命令提示符结束
			isQuickCommandResult := false

			// 检查命令结束标记
			for _, marker := range commandEndMarkers {
				if bytes.Contains(output, marker) {
					isCommandEnd = true
					// 如果输出中包含换行符并且以命令提示符结束，则认为是快速命令结果
					if bytes.Contains(output, []byte{'\n'}) &&
						bytes.Contains(output[max(0, len(output)-20):], marker) {
						isQuickCommandResult = true
					}
					break
				}
			}

			// 判断是否需要立即发送：
			// 1. 达到最大大小
			// 2. 已经过了较长时间
			// 3. 检测到命令完成且积累了一定数据
			// 4. 检测到快速命令结果
			if accumulated.Len() >= maxAccumulationSize ||
				time.Since(lastSendTime) > 500*time.Millisecond || // 确保每500ms至少发送一次
				(isCommandEnd && accumulated.Len() > 128) || // 检测到命令结束且积累了一定数据
				isQuickCommandResult { // 快速命令的结果（如ls）立即全部发送
				// 停止之前可能已启动的定时器
				if !flushTimer.Stop() {
					select {
					case <-flushTimer.C: // 清空通道
					default:
					}
				}
				sendAccumulatedOutput()
			} else if accumulated.Len() > 0 {
				// 如果有数据但没有立即发送，启动定时器以确保不会延迟太久
				// 先尝试停止定时器
				if !flushTimer.Stop() {
					select {
					case <-flushTimer.C: // 清空通道
					default:
					}
				}
				// 重置定时器
				flushTimer.Reset(maxAccumulationDelay)
			}

		case <-flushTimer.C:
			// 定时器触发，发送积累的数据
			sendAccumulatedOutput()

		case <-session.Done:
			// 会话已关闭，发送剩余数据
			sendAccumulatedOutput()
			return

		case <-stopCh:
			// 收到停止信号，可能是新的转发进程启动
			log.Printf("[MQTTY] 终止已有的输出转发 (会话: %s)", sessionID)
			// 发送剩余数据
			sendAccumulatedOutput()
			return
		}
	}
}

// 处理Nginx重载命令
func handleReloadCommand(client mqtt.Client, command struct {
	Command   string      `json:"command"`
	RequestId string      `json:"requestId"`
	ClientId  string      `json:"clientId"`
	Type      string      `json:"type"`
	SessionId string      `json:"sessionId"`
	Data      interface{} `json:"data"`
}, agentUuid string) {
	// 导入services包以调用ReloadNginx
	log.Printf("[MQTTY] 执行Nginx重载，clientId: %s, requestId: %s", command.ClientId, command.RequestId)

	// 调用Nginx重载服务
	result := services.ReloadNginx()
	log.Printf("[MQTTY] Nginx重载结果: %s", result)

	// 创建响应主题
	responseTopic := fmt.Sprintf("uranus/response/%s", agentUuid)
	log.Printf("[MQTTY] 发送响应到主题: %s", responseTopic)

	// 准备响应
	response := struct {
		Success   bool   `json:"success"`
		RequestId string `json:"requestId"`
		Command   string `json:"command"`
		Result    string `json:"result"`
		Message   string `json:"message,omitempty"`
	}{
		Success:   result == "OK",
		RequestId: command.RequestId,
		Command:   "reload",
		Result:    result,
		Message:   "Nginx配置已重载",
	}

	// 如果重载失败，更新消息
	if result != "OK" {
		response.Message = fmt.Sprintf("Nginx重载失败: %s", result)
	}

	// 发送响应
	respPayload, err := json.Marshal(response)
	if err != nil {
		log.Printf("[MQTTY] 序列化响应失败: %v", err)
		return
	}

	token := client.Publish(responseTopic, 1, false, respPayload)
	if token.Wait() && token.Error() != nil {
		log.Printf("[MQTTY] 发布响应失败: %v", token.Error())
	} else {
		log.Printf("[MQTTY] 响应已发送: %s", string(respPayload))
	}
}

// 处理Nginx启动命令
func handleStartCommand(client mqtt.Client, command struct {
	Command   string      `json:"command"`
	RequestId string      `json:"requestId"`
	ClientId  string      `json:"clientId"`
	Type      string      `json:"type"`
	SessionId string      `json:"sessionId"`
	Data      interface{} `json:"data"`
}, agentUuid string) {
	log.Printf("[MQTTY] 执行Nginx启动，clientId: %s, requestId: %s", command.ClientId, command.RequestId)

	// 调用Nginx启动服务
	result := services.StartNginx()
	log.Printf("[MQTTY] Nginx启动结果: %s", result)

	// 创建响应主题
	responseTopic := fmt.Sprintf("uranus/response/%s", agentUuid)
	log.Printf("[MQTTY] 发送响应到主题: %s", responseTopic)

	// 准备响应
	response := struct {
		Success   bool   `json:"success"`
		RequestId string `json:"requestId"`
		Command   string `json:"command"`
		Result    string `json:"result"`
		Message   string `json:"message,omitempty"`
	}{
		Success:   result == "OK",
		RequestId: command.RequestId,
		Command:   "start",
		Result:    result,
		Message:   "Nginx服务已启动",
	}

	// 如果启动失败，更新消息
	if result != "OK" {
		response.Message = fmt.Sprintf("Nginx启动失败: %s", result)
	}

	// 发送响应
	respPayload, err := json.Marshal(response)
	if err != nil {
		log.Printf("[MQTTY] 序列化响应失败: %v", err)
		return
	}

	token := client.Publish(responseTopic, 1, false, respPayload)
	if token.Wait() && token.Error() != nil {
		log.Printf("[MQTTY] 发布响应失败: %v", token.Error())
	} else {
		log.Printf("[MQTTY] 响应已发送: %s", string(respPayload))
	}
}

// 处理Nginx停止命令
func handleStopCommand(client mqtt.Client, command struct {
	Command   string      `json:"command"`
	RequestId string      `json:"requestId"`
	ClientId  string      `json:"clientId"`
	Type      string      `json:"type"`
	SessionId string      `json:"sessionId"`
	Data      interface{} `json:"data"`
}, agentUuid string) {
	log.Printf("[MQTTY] 执行Nginx停止，clientId: %s, requestId: %s", command.ClientId, command.RequestId)

	// 调用Nginx停止服务
	result := services.StopNginx()
	log.Printf("[MQTTY] Nginx停止结果: %s", result)

	// 创建响应主题
	responseTopic := fmt.Sprintf("uranus/response/%s", agentUuid)
	log.Printf("[MQTTY] 发送响应到主题: %s", responseTopic)

	// 准备响应
	response := struct {
		Success   bool   `json:"success"`
		RequestId string `json:"requestId"`
		Command   string `json:"command"`
		Result    string `json:"result"`
		Message   string `json:"message,omitempty"`
	}{
		Success:   result == "OK",
		RequestId: command.RequestId,
		Command:   "stop",
		Result:    result,
		Message:   "Nginx服务已停止",
	}

	// 如果停止失败，更新消息
	if result != "OK" {
		response.Message = fmt.Sprintf("Nginx停止失败: %s", result)
	}

	// 发送响应
	respPayload, err := json.Marshal(response)
	if err != nil {
		log.Printf("[MQTTY] 序列化响应失败: %v", err)
		return
	}

	token := client.Publish(responseTopic, 1, false, respPayload)
	if token.Wait() && token.Error() != nil {
		log.Printf("[MQTTY] 发布响应失败: %v", token.Error())
	} else {
		log.Printf("[MQTTY] 响应已发送: %s", string(respPayload))
	}
}

// 处理Nginx重启命令
func handleRestartCommand(client mqtt.Client, command struct {
	Command   string      `json:"command"`
	RequestId string      `json:"requestId"`
	ClientId  string      `json:"clientId"`
	Type      string      `json:"type"`
	SessionId string      `json:"sessionId"`
	Data      interface{} `json:"data"`
}, agentUuid string) {
	log.Printf("[MQTTY] 执行Nginx重启，clientId: %s, requestId: %s", command.ClientId, command.RequestId)

	// 先停止Nginx
	stopResult := services.StopNginx()
	log.Printf("[MQTTY] Nginx停止结果: %s", stopResult)

	// 如果停止失败，不再尝试启动
	if stopResult != "OK" {
		// 创建响应主题
		responseTopic := fmt.Sprintf("uranus/response/%s", agentUuid)
		// 准备响应
		response := struct {
			Success   bool   `json:"success"`
			RequestId string `json:"requestId"`
			Command   string `json:"command"`
			Result    string `json:"result"`
			Message   string `json:"message,omitempty"`
		}{
			Success:   false,
			RequestId: command.RequestId,
			Command:   "restart",
			Result:    stopResult,
			Message:   fmt.Sprintf("Nginx重启失败，无法停止服务: %s", stopResult),
		}

		// 发送响应
		respPayload, err := json.Marshal(response)
		if err != nil {
			log.Printf("[MQTTY] 序列化响应失败: %v", err)
			return
		}

		token := client.Publish(responseTopic, 1, false, respPayload)
		if token.Wait() && token.Error() != nil {
			log.Printf("[MQTTY] 发布响应失败: %v", token.Error())
		}
		return
	}

	// 等待一小段时间确保Nginx完全停止
	time.Sleep(500 * time.Millisecond)

	// 然后启动Nginx
	startResult := services.StartNginx()
	log.Printf("[MQTTY] Nginx启动结果: %s", startResult)

	// 创建响应主题
	responseTopic := fmt.Sprintf("uranus/response/%s", agentUuid)
	log.Printf("[MQTTY] 发送响应到主题: %s", responseTopic)

	// 准备响应
	response := struct {
		Success   bool   `json:"success"`
		RequestId string `json:"requestId"`
		Command   string `json:"command"`
		Result    string `json:"result"`
		Message   string `json:"message,omitempty"`
	}{
		Success:   startResult == "OK",
		RequestId: command.RequestId,
		Command:   "restart",
		Result:    startResult,
		Message:   "Nginx服务已重启",
	}

	// 如果启动失败，更新消息
	if startResult != "OK" {
		response.Message = fmt.Sprintf("Nginx重启失败，无法启动服务: %s", startResult)
	}

	// 发送响应
	respPayload, err := json.Marshal(response)
	if err != nil {
		log.Printf("[MQTTY] 序列化响应失败: %v", err)
		return
	}

	token := client.Publish(responseTopic, 1, false, respPayload)
	if token.Wait() && token.Error() != nil {
		log.Printf("[MQTTY] 发布响应失败: %v", token.Error())
	} else {
		log.Printf("[MQTTY] 响应已发送: %s", string(respPayload))
	}
}

// 清理过期的输出缓存
func cleanupRecentOutputs() {
	// 清理超过5秒的输出缓存
	now := time.Now()
	toDelete := make([]string, 0, 10) // 预分配一个合理大小的切片

	// 先收集要删除的key（使用写锁）
	recentOutputsMutex.Lock()
	defer recentOutputsMutex.Unlock()

	for hash, timestamp := range recentOutputs {
		if now.Sub(timestamp) > 5*time.Second {
			toDelete = append(toDelete, hash)
		}
	}

	// 批量删除，减少map操作次数
	for _, hash := range toDelete {
		delete(recentOutputs, hash)
	}

	// 如果缓存太大，只保留最新的一半条目
	if len(recentOutputs) > maxRecentOutputs {
		// 创建一个时间排序的条目数组
		entries := make([]struct {
			hash string
			time time.Time
		}, 0, len(recentOutputs))

		for hash, t := range recentOutputs {
			entries = append(entries, struct {
				hash string
				time time.Time
			}{hash, t})
		}

		// 按时间排序
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].time.Before(entries[j].time)
		})

		// 删除最旧的一半
		halfSize := len(entries) / 2
		for i := 0; i < halfSize; i++ {
			delete(recentOutputs, entries[i].hash)
		}
	}
}
