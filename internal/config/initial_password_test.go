package config

import "testing"

func TestGenerateInitialPassword(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		p := generateInitialPassword()
		if len(p) != 20 {
			t.Fatalf("期望 20 个字符，得到 %d: %q", len(p), p)
		}
		if seen[p] {
			t.Fatalf("初始密码重复: %q", p)
		}
		seen[p] = true
	}
}
