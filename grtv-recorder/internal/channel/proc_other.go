//go:build !windows

package channel

import "os/exec"

// hideWindow não faz nada fora do Windows (build de desenvolvimento/testes).
func hideWindow(cmd *exec.Cmd) {}
