// Package channel implementa o supervisor de processo ffmpeg por canal e a
// máquina de estados do watchdog (SPEC.md §5, §8).
package channel

import (
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"strconv"
	"strings"

	"grtv-recorder/internal/config"
)

// BuildArgs monta os argumentos do ffmpeg para o canal, escolhendo o bloco de entrada
// HLS ou RTSP conforme o esquema da URL (SPEC.md §5.1, §5.2, §5.3).
// outputPattern é o caminho completo com o padrão -strftime, ex.:
// "G:\Sistema\ftp\work\TvTropical\%Y-%m-%d_%H.%M.%S.ts".
func BuildArgs(ch config.Channel, outputPattern string, segmentSeconds int) ([]string, error) {
	u, err := url.Parse(ch.URL)
	if err != nil {
		return nil, fmt.Errorf("canal %q: url inválida: %w", ch.Name, err)
	}

	var inputArgs []string
	switch scheme := strings.ToLower(u.Scheme); scheme {
	case "http", "https":
		// SPEC.md §5.1 — HLS, padrão do projeto.
		inputArgs = []string{
			"-reconnect", "1",
			"-reconnect_streamed", "1",
			"-reconnect_on_network_error", "1",
			"-reconnect_delay_max", "5",
			"-http_persistent", "1",
			"-rw_timeout", "15000000",
			"-i", ch.URL,
		}
	case "rtsp":
		// SPEC.md §5.2 — RTSP, fallback.
		// Nota: em builds ffmpeg < 6.0 a flag se chama "-stimeout", não "-timeout".
		inputArgs = []string{
			"-rtsp_transport", "tcp",
			"-timeout", "15000000",
			"-i", ch.URL,
		}
	default:
		return nil, fmt.Errorf("canal %q: esquema de url não suportado %q (use http/https ou rtsp)", ch.Name, scheme)
	}

	args := []string{"-nostdin", "-hide_banner", "-loglevel", "warning"}
	args = append(args, inputArgs...)
	args = append(args,
		"-map", "0:v:0", "-map", "0:a:0?",
		"-c", "copy",
		"-f", "segment",
		"-segment_time", strconv.Itoa(segmentSeconds),
		"-segment_atclocktime", "1",
		"-reset_timestamps", "1",
		"-segment_format", "mpegts",
		"-strftime", "1",
		outputPattern,
	)
	return args, nil
}

// Start inicia o processo ffmpeg com stdin conectado (para o shutdown limpo via "q",
// SPEC.md §8 tabela) e stderr redirecionado para o log do canal.
func Start(ffmpegPath string, args []string, stderr io.Writer) (*exec.Cmd, io.WriteCloser, error) {
	cmd := exec.Command(ffmpegPath, args...)
	cmd.Stderr = stderr
	hideWindow(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("stdin pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("iniciar ffmpeg: %w", err)
	}

	return cmd, stdin, nil
}

// CommandLine devolve a linha de comando completa, para log (SPEC.md §11 "start/restart
// de cada ffmpeg, com o comando completo").
func CommandLine(ffmpegPath string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, ffmpegPath)
	parts = append(parts, args...)
	return strings.Join(parts, " ")
}
