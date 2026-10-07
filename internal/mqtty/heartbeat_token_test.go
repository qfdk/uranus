package mqtty

import (
	"encoding/json"
	"strings"
	"testing"
)

// 心跳会以明文发布到共享主题，不能包含命令鉴权用的 token。
func TestHeartbeatDoesNotContainToken(t *testing.T) {
	data, err := json.Marshal(HeartbeatData{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(data)), "token") {
		t.Fatalf("心跳载荷不应包含 token: %s", data)
	}
}
