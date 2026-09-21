// Package config carrega e valida config.yaml (SPEC.md §9).
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// invalidNameChars são os caracteres proibidos em nome de canal (viram nome de pasta).
const invalidNameChars = `\/:*?"<>|`

// Channel é um canal de TV a gravar (SPEC.md §9, §12).
type Channel struct {
	Name    string `yaml:"name"`
	Label   string `yaml:"label"`
	URL     string `yaml:"url"`
	Enabled bool   `yaml:"enabled"`

	// AVSyncOffsetSeconds corrige um offset fixo entre áudio e vídeo já presente na
	// fonte (comum em encoders IP baratos). Positivo atrasa o áudio (use quando o
	// áudio chega adiantado em relação ao vídeo, caso mais comum); negativo adianta o
	// áudio. Aplicado só no remux via -itsoffset, sem decodificar/reencodar nenhum
	// stream — não é deriva de clock (isso exigiria reencodar o áudio, o que o
	// SPEC.md proíbe). Default 0 (sem correção).
	AVSyncOffsetSeconds float64 `yaml:"av_sync_offset_seconds"`
}

// Config é o config.yaml completo (SPEC.md §9).
type Config struct {
	FfmpegPath  string `yaml:"ffmpeg_path"`
	FfprobePath string `yaml:"ffprobe_path"`
	WorkDir     string `yaml:"work_dir"`
	OutputDir   string `yaml:"output_dir"`
	LogDir      string `yaml:"log_dir"`

	SegmentSeconds  int `yaml:"segment_seconds"`
	GridSnapSeconds int `yaml:"grid_snap_seconds"`

	StallTimeoutSeconds   int `yaml:"stall_timeout_seconds"`
	BackoffInitialSeconds int `yaml:"backoff_initial_seconds"`
	BackoffMaxSeconds     int `yaml:"backoff_max_seconds"`
	OrphanTimeoutSeconds  int `yaml:"orphan_timeout_seconds"`

	PublishTickSeconds int `yaml:"publish_tick_seconds"`
	MaxPublishRetries  int `yaml:"max_publish_retries"`

	PreCreateDayFolders bool   `yaml:"pre_create_day_folders"`
	LogLevel            string `yaml:"log_level"`

	Channels []Channel `yaml:"channels"`
}

// QuarantineDir é a pasta de arquivos inválidos dentro de work_dir (SPEC.md §4).
func (c *Config) QuarantineDir() string {
	return filepath.Join(c.WorkDir, "_quarantine")
}

// ChannelWorkDir é a pasta plana onde o ffmpeg do canal escreve .ts (SPEC.md §4).
func (c *Config) ChannelWorkDir(channelName string) string {
	return filepath.Join(c.WorkDir, channelName)
}

// Load lê e faz parse do arquivo YAML em path. Não valida — chame Validate em seguida.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("ler config %q: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}

	applyDefaults(&cfg)
	return &cfg, nil
}

// applyDefaults preenche valores default para campos ausentes no YAML.
func applyDefaults(cfg *Config) {
	if cfg.SegmentSeconds == 0 {
		cfg.SegmentSeconds = 240
	}
	if cfg.GridSnapSeconds == 0 {
		cfg.GridSnapSeconds = 3
	}
	if cfg.StallTimeoutSeconds == 0 {
		cfg.StallTimeoutSeconds = 300
	}
	if cfg.BackoffInitialSeconds == 0 {
		cfg.BackoffInitialSeconds = 2
	}
	if cfg.BackoffMaxSeconds == 0 {
		cfg.BackoffMaxSeconds = 60
	}
	if cfg.OrphanTimeoutSeconds == 0 {
		cfg.OrphanTimeoutSeconds = 300
	}
	if cfg.PublishTickSeconds == 0 {
		cfg.PublishTickSeconds = 5
	}
	if cfg.MaxPublishRetries == 0 {
		cfg.MaxPublishRetries = 3
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
}

// Validate aplica as regras do SPEC.md §9 "Validação no arranque".
// Retorna erro fatal descritivo na primeira violação encontrada.
func (c *Config) Validate() error {
	if c.FfmpegPath == "" {
		return fmt.Errorf("ffmpeg_path não pode ser vazio")
	}
	if c.FfprobePath == "" {
		return fmt.Errorf("ffprobe_path não pode ser vazio")
	}
	if c.WorkDir == "" {
		return fmt.Errorf("work_dir não pode ser vazio")
	}
	if c.OutputDir == "" {
		return fmt.Errorf("output_dir não pode ser vazio")
	}
	if c.LogDir == "" {
		return fmt.Errorf("log_dir não pode ser vazio")
	}

	if c.SegmentSeconds <= 0 || 3600%c.SegmentSeconds != 0 {
		return fmt.Errorf("segment_seconds=%d deve dividir 3600 exatamente", c.SegmentSeconds)
	}

	switch strings.ToLower(c.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log_level=%q inválido (use debug|info|warn|error)", c.LogLevel)
	}

	if len(c.Channels) == 0 {
		return fmt.Errorf("nenhum canal configurado")
	}

	seen := make(map[string]bool, len(c.Channels))
	for i, ch := range c.Channels {
		if ch.Name == "" {
			return fmt.Errorf("canal[%d]: name não pode ser vazio", i)
		}
		if strings.ContainsAny(ch.Name, invalidNameChars) {
			return fmt.Errorf("canal[%d] name=%q contém caractere inválido (proibidos: %s)", i, ch.Name, invalidNameChars)
		}
		if seen[ch.Name] {
			return fmt.Errorf("nome de canal duplicado: %q", ch.Name)
		}
		seen[ch.Name] = true

		if ch.URL == "" {
			return fmt.Errorf("canal %q: url não pode ser vazia", ch.Name)
		}
		u, err := url.Parse(ch.URL)
		if err != nil {
			return fmt.Errorf("canal %q: url inválida: %w", ch.Name, err)
		}
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "rtsp":
		default:
			return fmt.Errorf("canal %q: esquema de url não suportado %q (use http/https ou rtsp)", ch.Name, u.Scheme)
		}
	}

	sameVol, err := sameVolume(c.WorkDir, c.OutputDir)
	if err != nil {
		return fmt.Errorf("verificar volume de work_dir/output_dir: %w", err)
	}
	if !sameVol {
		return fmt.Errorf("work_dir (%s) e output_dir (%s) devem estar no MESMO volume — publicação depende de os.Rename atômico (SPEC.md §4)", c.WorkDir, c.OutputDir)
	}

	return nil
}

// sameVolume compara a letra de unidade (Windows) ou raiz do caminho.
// Em SO sem noção de "volume" (ex.: Linux em dev), sempre retorna true.
func sameVolume(a, b string) (bool, error) {
	va := filepath.VolumeName(filepath.Clean(a))
	vb := filepath.VolumeName(filepath.Clean(b))
	if va == "" && vb == "" {
		return true, nil
	}
	return strings.EqualFold(va, vb), nil
}
