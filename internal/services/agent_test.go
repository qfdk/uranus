package services

import (
	"os"
	"path/filepath"
	"testing"
	"uranus/internal/config"
)

// Agents upgraded from versions with a built-in MQTT broker have none in
// config.toml: they take the one the control center answers registration with.
func TestAdoptMQTTBroker(t *testing.T) {
	saved, _ := os.ReadFile(filepath.Join(fakeDir, "config.toml"))
	t.Cleanup(func() {
		os.WriteFile(filepath.Join(fakeDir, "config.toml"), saved, 0o600)
		config.ReloadConfig()
		MQTTBrokerAssigned = nil
	})
	started := 0
	MQTTBrokerAssigned = func() { started++ }

	adoptMQTTBroker([]byte(`{"mqttBroker":"javascript:alert(1)"}`))
	adoptMQTTBroker([]byte(`{"_id":"an old control center answers with the agent"}`))
	if config.GetAppConfig().MQTTBroker != "" || started != 0 {
		t.Fatalf("adopted an invalid answer: %q", config.GetAppConfig().MQTTBroker)
	}
	adoptMQTTBroker([]byte(`{"ok":true,"mqttBroker":"wss://mqtt.example.com/mqtt"}`))
	if got := config.GetAppConfig().MQTTBroker; got != "wss://mqtt.example.com/mqtt" || started != 1 {
		t.Fatalf("broker %q, MQTT started %d times", got, started)
	}
	adoptMQTTBroker([]byte(`{"ok":true,"mqttBroker":"wss://elsewhere.example.com/mqtt"}`))
	if got := config.GetAppConfig().MQTTBroker; got != "wss://mqtt.example.com/mqtt" || started != 1 {
		t.Fatalf("a configured broker was replaced by %q", got)
	}
}
