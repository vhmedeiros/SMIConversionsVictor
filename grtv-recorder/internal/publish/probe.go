package publish

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Duration roda ffprobe e retorna a duração do arquivo em segundos (SPEC.md §7.2 passos 1 e 3).
//
//	ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 <arquivo>
func Duration(ctx context.Context, ffprobePath, filePath string) (float64, error) {
	cmd := exec.CommandContext(ctx, ffprobePath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		filePath,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return 0, fmt.Errorf("ffprobe %q: %w (stderr: %s)", filePath, err, strings.TrimSpace(stderr.String()))
	}

	out := strings.TrimSpace(stdout.String())
	if out == "" || out == "N/A" {
		return 0, fmt.Errorf("ffprobe %q: duração ausente", filePath)
	}

	d, err := strconv.ParseFloat(out, 64)
	if err != nil {
		return 0, fmt.Errorf("ffprobe %q: duração inválida %q: %w", filePath, out, err)
	}
	return d, nil
}
