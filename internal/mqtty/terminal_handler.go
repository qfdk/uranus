package mqtty

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

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
		if err == nil && session != nil {
			// 会话存在，检查是否已关闭
			var isClosed bool
			select {
			case <-session.Done:
				isClosed = true
			default:
				isClosed = false
			}

			if !isClosed {
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

// 使用已发送消息缓存，防止重复消息
var recentOutputs = make(map[string]time.Time)
var maxRecentOutputs = 100 // 最多缓存100条最近消息

// forwardSessionOutputWithUUID 转发会话输出到指定的代理UUID
func forwardSessionOutputWithUUID(topicPrefix, sessionID string, manager *SessionManager, agentUuid string) {
	// 获取Agent UUID用于前端响应主题
	responseTopic := fmt.Sprintf("uranus/response/%s", agentUuid)

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

	for {
		select {
		case output, ok := <-session.Output:
			if !ok {
				// 通道已关闭
				return
			}

			// 打印输出的前30个字符以便于调试
			preview := string(output)
			if len(preview) > 30 {
				preview = preview[:30] + "..."
			}

			// 检查是否重复消息
			outputHash := fmt.Sprintf("%s-%d", sessionID, time.Now().UnixNano())

			// 创建标准MQTT消息
			message := Message{
				SessionID: sessionID,
				Type:      "output", // 添加类型信息，与前端对应
				Data:      string(output),
				Timestamp: time.Now().UnixNano() / 1e6,
			}

			payload, err := json.Marshal(message)
			if err != nil {
				log.Printf("[MQTTY] 序列化输出消息失败: %v", err)
				continue
			}

			// 添加到最近消息缓存
			recentOutputs[outputHash] = time.Now()

			// 发布到前端响应主题
			if mqttClient != nil && mqttClient.IsConnected() {
				token := mqttClient.Publish(responseTopic, 1, false, payload)
				if token.Wait() && token.Error() != nil {
					log.Printf("[MQTTY] 发布输出消息失败: %v", token.Error())
				} else {
					log.Printf("[MQTTY] 发送会话输出 (%s): %q", sessionID, preview)
				}
			}

		case <-session.Done:
			// 会话已关闭
			return

		case <-stopCh:
			// 收到停止信号，可能是新的转发进程启动
			log.Printf("[MQTTY] 终止已有的输出转发 (会话: %s)", sessionID)
			return
		}
	}
}

// 清理过期的输出缓存
func cleanupRecentOutputs() {
	// 清理超过5秒的输出缓存
	now := time.Now()
	for hash, timestamp := range recentOutputs {
		if now.Sub(timestamp) > 5*time.Second {
			delete(recentOutputs, hash)
		}
	}

	// 如果缓存太大，删除最旧的一些条目
	if len(recentOutputs) > maxRecentOutputs {
		// 简单处理：直接清空缓存
		recentOutputs = make(map[string]time.Time)
	}
}
