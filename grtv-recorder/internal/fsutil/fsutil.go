// Package fsutil contém helpers de sistema de arquivos usados pelo publisher:
// criação de pastas, rename atômico e sufixo anticolisão (SPEC.md §7.2 passo 6-7).
package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// MkdirAll cria dir (e pais) se não existir.
func MkdirAll(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("MkdirAll %q: %w", dir, err)
	}
	return nil
}

// UniqueDestination retorna dst se ele não existir, ou dst com sufixo "_1", "_2", ...
// caso já exista — nunca sobrescreve (SPEC.md §7.2 passo 6).
func UniqueDestination(dst string) (string, error) {
	if _, err := os.Stat(dst); os.IsNotExist(err) {
		return dst, nil
	} else if err != nil {
		return "", fmt.Errorf("stat %q: %w", dst, err)
	}

	ext := filepath.Ext(dst)
	base := dst[:len(dst)-len(ext)]
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s_%d%s", base, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil {
			return "", fmt.Errorf("stat %q: %w", candidate, err)
		}
	}
}

// Rename move src para dst atomicamente (mesmo volume — SPEC.md §4 regra 2).
// dst deve já ter sido resolvido via UniqueDestination.
func Rename(src, dst string) error {
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("rename %q -> %q: %w", src, dst, err)
	}
	return nil
}
