package tools

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// SecureCommand 安全命令结构
type SecureCommand struct {
	Type      string `json:"type"`
	Payload   string `json:"payload"`
	Timestamp int64  `json:"timestamp"`
}

// CommandWithMeta 带元数据的命令结构
type CommandWithMeta struct {
	Action    string                 `json:"action"`
	Config    map[string]interface{} `json:"config,omitempty"`
	AgentUuid string                 `json:"agentUuid,omitempty"`
	Timestamp int64                  `json:"timestamp"`
	Nonce     string                 `json:"nonce"`
}

// Encrypt 使用AES-256-GCM加密数据
func Encrypt(text, key string) (string, error) {
	// 创建密钥hash (确保是32字节)
	keyHash := sha256.Sum256([]byte(key))

	// 创建AES加密器
	block, err := aes.NewCipher(keyHash[:])
	if err != nil {
		return "", err
	}

	// 创建GCM模式
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	// 生成随机nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	// 加密数据
	ciphertext := gcm.Seal(nonce, nonce, []byte(text), nil)

	// 返回hex编码的加密数据
	return hex.EncodeToString(ciphertext), nil
}

// Decrypt 使用AES-256-GCM解密数据
func Decrypt(encryptedData, key string) (string, error) {
	// 创建密钥hash
	keyHash := sha256.Sum256([]byte(key))

	// 解码hex数据
	data, err := hex.DecodeString(encryptedData)
	if err != nil {
		return "", err
	}

	// 创建AES解密器
	block, err := aes.NewCipher(keyHash[:])
	if err != nil {
		return "", err
	}

	// 创建GCM模式
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	// 检查数据长度
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	// 分离nonce和密文
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]

	// 解密数据
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// ParseSecureCommand 解析安全命令
func ParseSecureCommand(message []byte, agentToken string, timeWindow time.Duration) (*CommandWithMeta, error) {
	// 解析消息
	var secureCmd SecureCommand
	if err := json.Unmarshal(message, &secureCmd); err != nil {
		return nil, fmt.Errorf("invalid message format: %v", err)
	}

	// 检查命令类型
	if secureCmd.Type != "secure_command" {
		return nil, errors.New("invalid command type")
	}

	// 解密负载
	decryptedPayload, err := Decrypt(secureCmd.Payload, agentToken)
	if err != nil {
		return nil, fmt.Errorf("decryption failed: %v", err)
	}

	// 解析命令
	var command CommandWithMeta
	if err := json.Unmarshal([]byte(decryptedPayload), &command); err != nil {
		return nil, fmt.Errorf("invalid command format: %v", err)
	}

	// 验证时间窗口
	now := time.Now().UnixMilli()
	if now-command.Timestamp > timeWindow.Milliseconds() {
		return nil, errors.New("command expired")
	}

	return &command, nil
}

// EncryptCommand 加密命令 (主要用于测试)
func EncryptCommand(command *CommandWithMeta, agentToken string) (*SecureCommand, error) {
	// 序列化命令
	commandBytes, err := json.Marshal(command)
	if err != nil {
		return nil, err
	}

	// 加密命令
	encryptedPayload, err := Encrypt(string(commandBytes), agentToken)
	if err != nil {
		return nil, err
	}

	return &SecureCommand{
		Type:      "secure_command",
		Payload:   encryptedPayload,
		Timestamp: time.Now().UnixMilli(),
	}, nil
}

// GenerateNonce 生成随机nonce
func GenerateNonce() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
