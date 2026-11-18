package services

import (
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// When uranus is not a systemd unit, `systemctl restart uranus.service` fails
// but `systemctl daemon-reload` succeeds. That used to count as a successful
// restart, so new settings (token, MQTT broker) never took effect.
func TestRestartAgentFallsBackToSelfRestart(t *testing.T) {
	bin := t.TempDir()
	scripts := map[string]string{
		"systemctl": "#!/bin/sh\n[ \"$1\" = daemon-reload ] && exit 0\necho 'Unit uranus.service not found.' >&2\nexit 5\n",
		"service":   "#!/bin/sh\nexit 1\n",
	}
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	// The fallback restarts through tableflip by sending SIGUSR2 to the process itself.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGUSR2)
	defer signal.Stop(sig)

	if err := RestartAgent(); err != nil {
		t.Fatalf("RestartAgent: %v", err)
	}
	select {
	case <-sig:
	case <-time.After(2 * time.Second):
		t.Fatal("nothing restarted uranus")
	}
}
