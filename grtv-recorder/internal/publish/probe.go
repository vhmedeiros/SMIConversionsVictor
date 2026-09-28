package publish

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Duration roda ffprobe e retorna a duração do CONTAINER em segundos (SPEC.md §7.2
// passo 1, validação do .ts bruto). Não usar no .mp4 já remuxado quando o canal tem
// av_sync_offset_seconds != 0 — nesse caso use VideoDuration (ver comentário lá).
//
//	ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 <arquivo>
func Duration(ctx context.Context, ffprobePath, filePath string) (float64, error) {
	return runProbeDuration(ctx, ffprobePath, formatDurationArgs(filePath))
}

// VideoDuration roda ffprobe e retorna a duração só da trilha de VÍDEO em segundos
// (SPEC.md §7.2 passo 3, duração real usada para nomear o bloco).
//
// Precisa ser a duração do vídeo, não do container: quando av_sync_offset_seconds !=
// 0, o remux (buildRemuxArgs) atrasa só o áudio via edit list, o que deixa a trilha de
// áudio — e portanto a duração do CONTAINER, que é o máximo entre as trilhas —
// avSyncOffset segundos mais longa que o vídeo de fato gravado. Nomear o bloco por
// essa duração inflada quebra o encosto na grade (SPEC.md §7.3) e o cálculo do
// horário de fim (SPEC.md §7.2 passo 5), deslocando o nome publicado por
// avSyncOffset segundos mesmo sem ter havido gap real.
//
//	ffprobe -v error -select_streams v:0 -show_entries stream=duration -of default=noprint_wrappers=1:nokey=1 <arquivo>
func VideoDuration(ctx context.Context, ffprobePath, filePath string) (float64, error) {
	return runProbeDuration(ctx, ffprobePath, videoDurationArgs(filePath))
}

func formatDurationArgs(filePath string) []string {
	return []string{
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		filePath,
	}
}

func videoDurationArgs(filePath string) []string {
	return []string{
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		filePath,
	}
}

func runProbeDuration(ctx context.Context, ffprobePath string, args []string) (float64, error) {
	filePath := args[len(args)-1]
	cmd := exec.CommandContext(ctx, ffprobePath, args...)
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
