// Package logging implementa os arquivos de log do SPEC.md §11:
// recorder.log (geral), gaps.log (interrupções) e ffmpeg\<Canal>.log (stderr).
package logging

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Level é o nível mínimo de log emitido.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// ParseLevel converte "debug|info|warn|error" (case-insensitive) em Level.
// Valor desconhecido cai em LevelInfo.
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "INFO"
	}
}

// Logger escreve em logs\recorder.log, rotativo (SPEC.md §11: 50MB, 10 arquivos, compressão).
// Formato de linha: 2026-09-18T00:04:03-04:00  INFO  [Globo]  mensagem  campo=valor
type Logger struct {
	file  *lumberjack.Logger
	out   io.Writer
	level Level
	mu    sync.Mutex
}

// New abre/rotaciona logs\recorder.log dentro de logDir. Writers extras em mirrors
// (ex.: os.Stdout, usado no modo console) recebem uma cópia de cada linha — útil para
// acompanhar o serviço rodando em foreground durante testes manuais.
func New(logDir string, level Level, mirrors ...io.Writer) *Logger {
	file := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "recorder.log"),
		MaxSize:    50, // MB
		MaxBackups: 10,
		Compress:   true,
	}

	var out io.Writer = file
	if len(mirrors) > 0 {
		out = io.MultiWriter(append([]io.Writer{file}, mirrors...)...)
	}

	return &Logger{file: file, out: out, level: level}
}

func (l *Logger) Close() error { return l.file.Close() }

func (l *Logger) Debug(channel, msg string, kv ...any) { l.log(LevelDebug, channel, msg, kv...) }
func (l *Logger) Info(channel, msg string, kv ...any)  { l.log(LevelInfo, channel, msg, kv...) }
func (l *Logger) Warn(channel, msg string, kv ...any)  { l.log(LevelWarn, channel, msg, kv...) }
func (l *Logger) Error(channel, msg string, kv ...any) { l.log(LevelError, channel, msg, kv...) }

func (l *Logger) log(lvl Level, channel, msg string, kv ...any) {
	if lvl < l.level {
		return
	}
	ts := time.Now().Format(time.RFC3339)
	line := fmt.Sprintf("%s  %-5s  [%s]  %s", ts, lvl.String(), channel, msg)
	if fields := formatFields(kv); fields != "" {
		line += "  " + fields
	}
	line += "\n"

	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.out.Write([]byte(line))
}

// formatFields junta pares key,value em "key=value key2=value2".
// Um número ímpar de argumentos descarta o último (sem par).
func formatFields(kv []any) string {
	if len(kv) == 0 {
		return ""
	}
	var b strings.Builder
	for i := 0; i+1 < len(kv); i += 2 {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%v=%v", kv[i], kv[i+1])
	}
	return b.String()
}

// GapsLogger escreve logs\gaps.log (SPEC.md §8.1), uma linha por interrupção registrada.
type GapsLogger struct {
	out io.WriteCloser
	mu  sync.Mutex
}

// NewGapsLogger abre/rotaciona logs\gaps.log dentro de logDir.
func NewGapsLogger(logDir string) *GapsLogger {
	return &GapsLogger{
		out: &lumberjack.Logger{
			Filename:   filepath.Join(logDir, "gaps.log"),
			MaxSize:    10, // MB
			MaxBackups: 10,
			Compress:   false,
		},
	}
}

func (g *GapsLogger) Close() error { return g.out.Close() }

// Record grava uma linha de gap:
// 2026-09-18T11:07:14-04:00  Sbt  GAP  inicio=11:07:03 fim=11:12:00 duracao=297s motivo=process_exit code=1
func (g *GapsLogger) Record(channel string, start, end time.Time, motivo string) {
	ts := time.Now().Format(time.RFC3339)
	dur := end.Sub(start).Seconds()
	if dur < 0 {
		dur = 0
	}
	line := fmt.Sprintf("%s  %s  GAP  inicio=%s fim=%s duracao=%.0fs motivo=%s\n",
		ts, channel, start.Format("15:04:05"), end.Format("15:04:05"), dur, motivo)

	g.mu.Lock()
	defer g.mu.Unlock()
	_, _ = g.out.Write([]byte(line))
}

// NewFfmpegWriter cria o destino do stderr do ffmpeg de um canal:
// logs\ffmpeg\<Canal>.log, rotativo (SPEC.md §11: 10MB, 3 arquivos).
func NewFfmpegWriter(logDir, channelName string) io.WriteCloser {
	return &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "ffmpeg", channelName+".log"),
		MaxSize:    10, // MB
		MaxBackups: 3,
		Compress:   false,
	}
}
