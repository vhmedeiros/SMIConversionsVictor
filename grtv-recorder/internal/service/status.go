package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ChannelStatus é o estado de um canal exposto em logs\status.json.
type ChannelStatus struct {
	Name      string    `json:"name"`
	State     string    `json:"state"`
	LastError string    `json:"last_error,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Status é o snapshot completo, escrito pelo serviço e lido pelo subcomando `status`
// (SPEC.md §10.1: "grtv-recorder.exe status → estado dos 8 canais").
type Status struct {
	UpdatedAt time.Time       `json:"updated_at"`
	Channels  []ChannelStatus `json:"channels"`
}

func statusPath(logDir string) string {
	return filepath.Join(logDir, "status.json")
}

// WriteStatus grava o snapshot atomicamente (escreve em .tmp e renomeia).
func WriteStatus(logDir string, st Status) error {
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("MkdirAll %q: %w", logDir, err)
	}

	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal status: %w", err)
	}

	dst := statusPath(logDir)
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("escrever %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("rename %q -> %q: %w", tmp, dst, err)
	}
	return nil
}

// ReadStatus lê o último snapshot gravado pelo serviço em execução.
func ReadStatus(logDir string) (Status, error) {
	var st Status
	data, err := os.ReadFile(statusPath(logDir))
	if err != nil {
		return st, fmt.Errorf("ler %q: %w", statusPath(logDir), err)
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return st, fmt.Errorf("parse %q: %w", statusPath(logDir), err)
	}
	return st, nil
}
