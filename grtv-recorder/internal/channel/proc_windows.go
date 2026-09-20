//go:build windows

package channel

import (
	"os/exec"
	"syscall"
)

// hideWindow evita que uma janela de console pisque para cada um dos 8 processos
// ffmpeg quando rodando em modo serviço/console.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
	}
}
