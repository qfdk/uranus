package services

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const newCommit = "1234567890abcdef1234567890abcdef12345678"

// release publishes a signed version.json and a program, like scripts/release.sh.
type release struct {
	key      ed25519.PrivateKey
	manifest []byte
	sig      string
	program  []byte
}

func newRelease(t *testing.T, program string) *release {
	t.Helper()
	pub, key, _ := ed25519.GenerateKey(nil)
	savedKey, savedURL, savedPath := releasePublicKey, releaseURL, installPath
	releasePublicKey = base64.StdEncoding.EncodeToString(pub)
	installPath = t.TempDir()
	t.Cleanup(func() { releasePublicKey, releaseURL, installPath = savedKey, savedURL, savedPath })

	r := &release{key: key, program: []byte(program)}
	sum := sha256.Sum256(r.program)
	r.setManifest(map[string]any{
		"commitId": newCommit, "buildTime": "2026-11-19 10:00:00", "buildVersion": "v1.0.0", "release": 200,
		"files": map[string]any{"uranus-" + runtime.GOARCH: map[string]any{"sha256": hex.EncodeToString(sum[:]), "size": len(r.program)}},
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/version.json":
			w.Write(r.manifest)
		case "/version.json.sig":
			w.Write([]byte(r.sig))
		case "/uranus-" + runtime.GOARCH:
			w.Write(r.program)
		default:
			http.NotFound(w, req)
		}
	}))
	t.Cleanup(srv.Close)
	releaseURL = srv.URL
	return r
}

func (r *release) setManifest(m map[string]any) {
	r.manifest, _ = json.Marshal(m)
	r.sig = base64.StdEncoding.EncodeToString(ed25519.Sign(r.key, r.manifest))
}

// A manifest counts only with the signature of the release key: whoever can
// change files on the release server must not get agents to install a program.
func TestFetchManifestChecksTheSignature(t *testing.T) {
	r := newRelease(t, "")
	if m, err := FetchManifest(); err != nil || m.CommitID != newCommit || m.Release != 200 {
		t.Fatalf("signed manifest: %+v %v", m, err)
	}
	r.manifest = []byte(strings.Replace(string(r.manifest), newCommit, "evil", 1))
	if _, err := FetchManifest(); err == nil {
		t.Fatal("a changed manifest was accepted")
	}
	r.sig = ""
	if _, err := FetchManifest(); err == nil {
		t.Fatal("an unsigned manifest was accepted")
	}
}

func installed(t *testing.T, name string) string {
	t.Helper()
	data, _ := os.ReadFile(filepath.Join(installPath, name))
	return string(data)
}

func prepareUpgrade(t *testing.T, program string) *release {
	t.Helper()
	simulateHost(t, "linux", false)
	r := newRelease(t, program)
	if err := os.WriteFile(filepath.Join(installPath, binaryName), []byte("old program"), 0o755); err != nil {
		t.Fatal(err)
	}
	return r
}

// The new program replaces the old one only after it was verified and ran;
// the old one stays as uranus.previous and the upgrade waits for confirmation.
func TestToUpdateProgram(t *testing.T) {
	program := "#!/bin/sh\necho 'Git commit: " + newCommit + "' >&2\n"
	prepareUpgrade(t, program)
	if err := ToUpdateProgram(); err != nil {
		t.Fatal(err)
	}
	if installed(t, binaryName) != program || installed(t, binaryName+".previous") != "old program" {
		t.Fatalf("installed %q, previous %q", installed(t, binaryName), installed(t, binaryName+".previous"))
	}
	for _, name := range []string{pendingUpgradeFile, upgradeTrigger} {
		if _, err := os.Stat(filepath.Join(installPath, name)); err != nil {
			t.Errorf("%s missing: %v", name, err)
		}
	}
}

func TestToUpdateProgramRefusesBadPrograms(t *testing.T) {
	for name, tc := range map[string]struct {
		program string
		change  func(r *release)
	}{
		// 篡改后的程序照样报出正确的提交号，只有 sha256 能发现
		"changed after signing": {"#!/bin/sh\necho " + newCommit + "\n", func(r *release) {
			r.program = []byte("#!/bin/sh\necho " + newCommit + "\n# 被人改过\n")
		}},
		"does not run":  {"#!/bin/sh\nexit 1\n", nil},
		"another build": {"#!/bin/sh\necho 'Git commit: 0000000'\n", nil},
	} {
		t.Run(name, func(t *testing.T) {
			r := prepareUpgrade(t, tc.program)
			if tc.change != nil {
				tc.change(r)
			}
			if err := ToUpdateProgram(); err == nil {
				t.Fatal("upgrade went ahead")
			}
			if installed(t, binaryName) != "old program" {
				t.Fatal("old program replaced")
			}
			if _, err := os.Stat(filepath.Join(installPath, pendingUpgradeFile)); err == nil {
				t.Fatal("upgrade recorded as pending")
			}
		})
	}
}

// A new program that keeps failing to start is replaced by the previous one,
// which then runs in its place.
func TestRollbackAfterFailedStarts(t *testing.T) {
	saved, savedReexec := installPath, reexec
	installPath = t.TempDir()
	t.Cleanup(func() { installPath, reexec = saved, savedReexec; upgradeConfirmed.Store(false) })
	exited := -1 // 换成上一版重新运行时记为 0
	reexec = func() error { exited = 0; return nil }
	os.WriteFile(filepath.Join(installPath, binaryName), []byte("broken new"), 0o755)
	os.WriteFile(filepath.Join(installPath, binaryName+".previous"), []byte("working old"), 0o755)

	writePendingUpgrade(pendingUpgrade{Commit: newCommit})
	for start := 1; start <= maxUpgradeAttempts; start++ {
		CheckPendingUpgrade()
		if exited != -1 || installed(t, binaryName) != "broken new" {
			t.Fatalf("start %d rolled back", start)
		}
	}
	CheckPendingUpgrade()
	if exited != 0 || installed(t, binaryName) != "working old" {
		t.Fatalf("not rolled back: exit %d, program %q", exited, installed(t, binaryName))
	}
	if _, err := os.Stat(filepath.Join(installPath, pendingUpgradeFile)); err == nil {
		t.Fatal("pending upgrade left behind")
	}

	// A start that gets ready confirms the upgrade.
	writePendingUpgrade(pendingUpgrade{Commit: newCommit})
	CheckPendingUpgrade()
	ConfirmUpgrade()
	if _, err := os.Stat(filepath.Join(installPath, pendingUpgradeFile)); err == nil {
		t.Fatal("confirmed upgrade still pending")
	}
}
