package mqtty

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
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
	// 如果没有指定shell，使用默认shell
	if shell == "" {
		shell = getDefaultShell()
	}

	cmd := exec.Command(shell)

	// 设置环境变量
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"PS1=\\[\\e[32m\\]\\u@\\h:\\[\\e[33m\\]\\w\\[\\e[0m\\]\\$ ")

	// 手动创建PTY master/slave对
	ptmx, tty, err := pty.Open()
	if err != nil {
		return nil, fmt.Errorf("无法打开PTY: %v", err)
	}

	// 设置命令的标准输入输出
	cmd.Stdin = tty
	cmd.Stdout = tty
	cmd.Stderr = tty

	// 将SysProcAttr设置为nil，完全不使用Setctty
	cmd.SysProcAttr = nil

	log.Printf("[MQTTY] 启动PTY，Shell: %s, OS: %s", shell, runtime.GOOS)

	// 启动命令
	if err := cmd.Start(); err != nil {
		tty.Close()
		ptmx.Close()
		return nil, fmt.Errorf("启动命令失败: %v", err)
	}

	// 关闭TTY的文件描述符，此时子进程已经继承了它
	tty.Close()

	log.Printf("[MQTTY] PTY启动成功，PID: %d", cmd.Process.Pid)

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
	var err error
	if p.File != nil {
		err = p.File.Close()
	}

	if p.Cmd != nil && p.Cmd.Process != nil {
		// 尝试终止进程组
		pgid, pgidErr := syscall.Getpgid(p.Cmd.Process.Pid)
		if pgidErr == nil {
			// 向进程组发送SIGTERM
			syscall.Kill(-pgid, syscall.SIGTERM)

			// 给进程组一些时间来清理资源
			go func() {
				time.Sleep(500 * time.Millisecond)
				if p.Cmd.ProcessState == nil || !p.Cmd.ProcessState.Exited() {
					// 如果进程还在运行，使用SIGKILL强制终止
					log.Printf("[MQTTY] 进程未正常退出，发送SIGKILL信号")
					syscall.Kill(-pgid, syscall.SIGKILL)
				}
			}()
		} else {
			// 无法获取进程组ID，直接终止进程
			log.Printf("[MQTTY] 获取进程组ID失败: %v，使用普通方式终止进程", pgidErr)
			p.Cmd.Process.Signal(syscall.SIGTERM)

			go func() {
				time.Sleep(500 * time.Millisecond)
				// 检查进程是否仍在运行
				if p.Cmd.ProcessState == nil || !p.Cmd.ProcessState.Exited() {
					log.Printf("[MQTTY] 进程未响应SIGTERM，强制终止")
					p.Cmd.Process.Kill()
				}
			}()
		}
	}

	return err
}
