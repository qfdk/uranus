// Package ptyshell runs interactive shells on a pseudo-terminal that is their
// controlling terminal. The kernel then delivers Ctrl+C to the foreground job,
// job control works, and closing the terminal hangs up the whole session, the
// same way as in a regular terminal emulator on Linux and macOS.
package ptyshell

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// killDelay is how long a hung-up shell may take to exit before its process
// group is killed.
const killDelay = 2 * time.Second

// Shell is an interactive shell attached to a pseudo-terminal.
type Shell struct {
	cmd       *exec.Cmd
	pty       *os.File
	exited    chan struct{}
	closeOnce sync.Once
}

// DefaultShell returns the first available shell among bash, sh and zsh.
func DefaultShell() string {
	for _, sh := range []string{"/bin/bash", "/bin/sh", "/bin/zsh"} {
		if info, err := os.Stat(sh); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return sh
		}
	}
	return "/bin/sh"
}

// Start launches shell (DefaultShell when empty) on a new pseudo-terminal of
// the given size, the way an SSH login does: as a login shell, so that it reads
// /etc/profile and the user's profile, in the home directory, with a clean
// environment.
func Start(shell string, rows, cols uint16) (*Shell, error) {
	if shell == "" {
		shell = DefaultShell()
	}
	env, home := loginEnv(shell)
	dir := home
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		dir = "/"
	}
	return start(shell, env, dir, rows, cols)
}

// keptEnv are the variables of the uranus process that a terminal inherits
// (LC_* too). The others belong to the service: systemd's INVOCATION_ID and
// JOURNAL_STREAM, GIN_MODE, PIDFILE, tableflip's variables...
var keptEnv = map[string]bool{"PATH": true, "LANG": true, "LANGUAGE": true, "TZ": true, "TMPDIR": true}

// loginEnv returns the environment of a login session running shell, and the
// user's home directory.
func loginEnv(shell string) (env []string, home string) {
	hasLocale := false
	for _, kv := range os.Environ() {
		name, value, _ := strings.Cut(kv, "=")
		if value == "" || !(keptEnv[name] || strings.HasPrefix(name, "LC_")) {
			continue
		}
		env = append(env, kv)
		if name == "LANG" || name == "LC_ALL" || name == "LC_CTYPE" {
			hasLocale = true
		}
	}
	// Without a locale the shell runs in the C locale: Chinese file names
	// are shown as ?? and multi-byte input breaks.
	if !hasLocale {
		env = append(env, "LANG=C.UTF-8")
	}
	if os.Getenv("PATH") == "" {
		env = append(env, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	}

	home = os.Getenv("HOME")
	name := os.Getenv("USER")
	if u, err := user.Current(); err == nil {
		if home == "" {
			home = u.HomeDir
		}
		name = u.Username
	}
	if home == "" {
		home = "/"
	}
	env = append(env,
		"HOME="+home,
		"USER="+name,
		"LOGNAME="+name,
		"SHELL="+shell,
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
	)
	return env, home
}

// start launches shell as a login shell with env in dir.
func start(shell string, env []string, dir string, rows, cols uint16) (*Shell, error) {
	ptmx, tty, err := pty.Open()
	if err != nil {
		return nil, err
	}
	// The child gets its own copies of the tty; the parent only keeps the master.
	defer tty.Close()

	if err := pty.Setsize(ptmx, &pty.Winsize{Rows: rows, Cols: cols}); err != nil {
		ptmx.Close()
		return nil, err
	}

	cmd := exec.Command(shell)
	// A leading "-" in argv[0] makes it a login shell, as login(1) and sshd do.
	cmd.Args = []string{"-" + filepath.Base(shell)}
	cmd.Env = env
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	// The shell leads a new session whose controlling terminal is the tty
	// (fd 0 in the child). pty.StartWithSize from creack/pty v1.1.9 can't be
	// used: it passes the parent's fd number as Ctty, which Go rejects since 1.15.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		ptmx.Close()
		return nil, err
	}

	s := &Shell{cmd: cmd, pty: ptmx, exited: make(chan struct{})}
	go func() {
		// Reap the shell so it doesn't stay around as a zombie.
		_ = cmd.Wait()
		close(s.exited)
	}()
	return s, nil
}

// PTY returns the master side of the pseudo-terminal: the shell's output is
// read from it, and user input (Ctrl+C included, as byte 0x03) is written to it.
func (s *Shell) PTY() *os.File { return s.pty }

// Pid returns the shell's process ID.
func (s *Shell) Pid() int { return s.cmd.Process.Pid }

// Exited is closed once the shell has exited.
func (s *Shell) Exited() <-chan struct{} { return s.exited }

// Resize changes the terminal size.
func (s *Shell) Resize(rows, cols uint16) error {
	return pty.Setsize(s.pty, &pty.Winsize{Rows: rows, Cols: cols})
}

// Close hangs up the terminal without waiting for the shell to exit. The shell
// gets SIGHUP and, as on a real hangup, bash passes it on to its jobs. If it is
// still running after killDelay, its process group is killed.
func (s *Shell) Close() {
	s.closeOnce.Do(func() {
		// Setsid made the shell the leader of its own process group.
		pgid := s.cmd.Process.Pid
		select {
		case <-s.exited:
		default:
			// The hangup is signalled explicitly: closing the master does not
			// do it reliably, because creack/pty leaves it in blocking mode and
			// the close waits for any pending read to return.
			_ = syscall.Kill(-pgid, syscall.SIGHUP)
		}

		go func() {
			select {
			case <-s.exited:
			case <-time.After(killDelay):
				_ = syscall.Kill(-pgid, syscall.SIGKILL)
			}
			// Close the master only once the shell is gone. A kernel hangup
			// while bash is still handling SIGHUP makes it exit (seen with
			// bash 5 on Linux) before it passes the hangup on to its jobs.
			_ = s.pty.Close()
		}()
	})
}
