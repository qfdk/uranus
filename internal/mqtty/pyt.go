package mqtty

import (
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// PTY 伪终端封装
type PTY struct {
	Cmd  *exec.Cmd
	File *os.File
}

// NewPTY 创建新的伪终端
func NewPTY(shell string) (*PTY, error) {
	cmd := exec.Command(shell)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Setsid:  true,
	}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

	return &PTY{
		Cmd:  cmd,
		File: ptmx,
	}, nil
}

// Resize 调整终端大小
func (p *PTY) Resize(rows, cols uint16) error {
	return pty.Setsize(p.File, &pty.Winsize{
		Rows: rows,
		Cols: cols,
	})
}

// Close 关闭PTY
func (p *PTY) Close() error {
	if err := p.File.Close(); err != nil {
		return err
	}

	if p.Cmd.Process != nil {
		pgid, err := syscall.Getpgid(p.Cmd.Process.Pid)
		if err == nil {
			syscall.Kill(-pgid, syscall.SIGTERM)

			go func() {
				time.Sleep(500 * time.Millisecond)
				if p.Cmd.ProcessState == nil || !p.Cmd.ProcessState.Exited() {
					syscall.Kill(-pgid, syscall.SIGKILL)
				}
			}()
		} else {
			p.Cmd.Process.Signal(syscall.SIGTERM)
			go func() {
				time.Sleep(500 * time.Millisecond)
				p.Cmd.Process.Kill()
			}()
		}
	}

	return nil
}
