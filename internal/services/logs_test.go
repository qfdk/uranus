package services

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestTailFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	var b strings.Builder
	for i := 1; i <= 20000; i++ {
		b.WriteString("line " + strconv.Itoa(i) + " " + strings.Repeat("x", 40) + "\n")
	}
	b.WriteString("bad \xff byte\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := TailFile(path, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "line 19999 ") || lines[2] != "bad � byte" {
		t.Fatalf("last lines: %q", lines)
	}
	// More lines than fit in the bytes read: whole lines only, the cut one dropped.
	lines, _ = TailFile(path, 100000)
	if len(lines) >= 20001 || !strings.HasPrefix(lines[0], "line ") || !strings.HasSuffix(lines[0], strings.Repeat("x", 40)) {
		t.Fatalf("got %d lines, first %q", len(lines), lines[0])
	}

	empty := filepath.Join(t.TempDir(), "empty.log")
	os.WriteFile(empty, nil, 0o644)
	if lines, err := TailFile(empty, 10); err != nil || len(lines) != 0 {
		t.Fatalf("empty file: %q %v", lines, err)
	}
}
