package publish

import "testing"

func TestFormatDurationArgs_ProbesContainer(t *testing.T) {
	got := formatDurationArgs("in.ts")
	want := []string{
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		"in.ts",
	}
	assertArgsEqual(t, got, want)
}

func TestVideoDurationArgs_ProbesVideoStreamOnly(t *testing.T) {
	// Precisa ser a trilha de vídeo, não o container: com av_sync_offset_seconds != 0
	// a trilha de áudio (e portanto format=duration) fica mais longa que o vídeo real
	// por causa do edit list de atraso (ver comentário de VideoDuration).
	got := videoDurationArgs("out.mp4")
	want := []string{
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		"out.mp4",
	}
	assertArgsEqual(t, got, want)
}
