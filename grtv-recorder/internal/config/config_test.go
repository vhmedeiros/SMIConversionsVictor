package config

import "testing"

func validConfig() *Config {
	return &Config{
		FfmpegPath:  "ffmpeg",
		FfprobePath: "ffprobe",
		WorkDir:     "/tmp/work",
		OutputDir:   "/tmp/arquivos",
		LogDir:      "/tmp/logs",

		SegmentSeconds: 240,
		LogLevel:       "info",
		Channels: []Channel{
			{Name: "Record", URL: "http://example/0.m3u8", Enabled: true},
			{Name: "Globo", URL: "http://example/2.m3u8", Enabled: true},
		},
	}
}

func TestValidate_OK(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_SegmentSecondsMustDivide3600(t *testing.T) {
	cfg := validConfig()
	cfg.SegmentSeconds = 7
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for segment_seconds not dividing 3600")
	}
}

func TestValidate_DuplicateChannelName(t *testing.T) {
	cfg := validConfig()
	cfg.Channels = append(cfg.Channels, Channel{Name: "Record", URL: "http://x/1.m3u8", Enabled: true})
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for duplicate channel name")
	}
}

func TestValidate_InvalidChannelNameChars(t *testing.T) {
	cfg := validConfig()
	cfg.Channels[0].Name = "Rec/ord"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for invalid channel name characters")
	}
}

func TestValidate_EmptyURL(t *testing.T) {
	cfg := validConfig()
	cfg.Channels[0].URL = ""
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for empty url")
	}
}

func TestValidate_UnsupportedScheme(t *testing.T) {
	cfg := validConfig()
	cfg.Channels[0].URL = "ftp://example/0"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for unsupported url scheme")
	}
}
