package channel

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"grtv-recorder/internal/config"
	"grtv-recorder/internal/fsutil"
	"grtv-recorder/internal/logging"
)

// State é o estado do supervisor de processo (SPEC.md §8).
type State string

const (
	StateStopped State = "STOPPED"
	StateRunning State = "RUNNING"
	StateBackoff State = "BACKOFF"
)

// watchdogPollInterval é a frequência de checagem de stall — bem menor que o próprio
// stall_timeout_seconds para detectar o travamento com folga.
const watchdogPollInterval = 5 * time.Second

// gracefulStopTimeout é quanto o worker espera o ffmpeg sair sozinho após enviar "q"
// no stdin, antes de matar o processo (SPEC.md §8 tabela, linha 3).
const gracefulStopTimeout = 10 * time.Second

// stableRunThreshold é o tempo de execução acima do qual o backoff é resetado
// (SPEC.md §8 tabela: "Reset do backoff após 120s de execução estável").
const stableRunThreshold = 120 * time.Second

// Snapshot é o estado observável de um worker, usado pelo subcomando `status`.
type Snapshot struct {
	Name      string
	State     State
	LastError string
	UpdatedAt time.Time
}

// Worker supervisiona o processo ffmpeg de um canal: start/monitor/restart com
// backoff e watchdog de stream travado (SPEC.md §8).
type Worker struct {
	cfg       *config.Config
	ch        config.Channel
	workDir   string
	logger    *logging.Logger
	gaps      *logging.GapsLogger
	ffmpegLog io.WriteCloser

	mu        sync.Mutex
	state     State
	lastError string
	updatedAt time.Time
}

// NewWorker cria o worker de um canal.
func NewWorker(cfg *config.Config, ch config.Channel, logger *logging.Logger, gaps *logging.GapsLogger, ffmpegLog io.WriteCloser) *Worker {
	return &Worker{
		cfg:       cfg,
		ch:        ch,
		workDir:   cfg.ChannelWorkDir(ch.Name),
		logger:    logger,
		gaps:      gaps,
		ffmpegLog: ffmpegLog,
		state:     StateStopped,
		updatedAt: time.Now(),
	}
}

// Snapshot devolve o estado atual (thread-safe), para o subcomando `status`.
func (w *Worker) Snapshot() Snapshot {
	w.mu.Lock()
	defer w.mu.Unlock()
	return Snapshot{Name: w.ch.Name, State: w.state, LastError: w.lastError, UpdatedAt: w.updatedAt}
}

func (w *Worker) setState(s State, lastErr string) {
	w.mu.Lock()
	w.state = s
	w.lastError = lastErr
	w.updatedAt = time.Now()
	w.mu.Unlock()
}

// outputPattern é o caminho completo com -strftime para este canal (SPEC.md §5.1).
func (w *Worker) outputPattern() string {
	return filepath.Join(w.workDir, "%Y-%m-%d_%H.%M.%S.ts")
}

// Run é o loop de supervisão: start -> monitor (exit natural | stall | ctx cancelado)
// -> backoff -> repete. Bloqueia até ctx ser cancelado.
func (w *Worker) Run(ctx context.Context) {
	if err := fsutil.MkdirAll(w.workDir); err != nil {
		w.logger.Error(w.ch.Name, "falha ao criar pasta de trabalho", "err", err)
	}

	backoff := time.Duration(w.cfg.BackoffInitialSeconds) * time.Second
	maxBackoff := time.Duration(w.cfg.BackoffMaxSeconds) * time.Second

	for ctx.Err() == nil {
		args, err := BuildArgs(w.ch, w.outputPattern(), w.cfg.SegmentSeconds)
		if err != nil {
			w.logger.Error(w.ch.Name, "comando ffmpeg inválido, canal não pode ser iniciado", "err", err)
			w.setState(StateStopped, err.Error())
			return
		}

		w.logger.Info(w.ch.Name, "iniciando ffmpeg", "cmd", CommandLine(w.cfg.FfmpegPath, args))
		startedAt := time.Now()
		cmd, stdin, err := Start(w.cfg.FfmpegPath, args, w.ffmpegLog)

		var ran time.Duration
		if err != nil {
			w.logger.Error(w.ch.Name, "falha ao iniciar ffmpeg", "err", err)
			w.setState(StateBackoff, err.Error())
		} else {
			w.setState(StateRunning, "")
			var reason string
			ran, reason = w.superviseOne(ctx, cmd, stdin, startedAt)
			if ctx.Err() != nil {
				w.setState(StateStopped, "")
				return
			}
			w.setState(StateBackoff, reason)
		}

		if ran >= stableRunThreshold {
			backoff = time.Duration(w.cfg.BackoffInitialSeconds) * time.Second
		}

		w.logger.Info(w.ch.Name, "aguardando para reiniciar", "backoff_s", int(backoff.Seconds()))
		if !sleepCtx(ctx, backoff) {
			w.setState(StateStopped, "")
			return
		}
		backoff = nextBackoff(backoff, maxBackoff)
	}
	w.setState(StateStopped, "")
}

// superviseOne acompanha um processo ffmpeg já iniciado até ele sair — por término
// natural, por stall detectado pelo watchdog, ou por cancelamento do contexto (shutdown
// do serviço) — e retorna por quanto tempo ele rodou e o motivo (para logs\gaps.log e
// para status.json — sem isso, diagnosticar exigia abrir o log de stderr do ffmpeg).
func (w *Worker) superviseOne(ctx context.Context, cmd *exec.Cmd, stdin io.WriteCloser, startedAt time.Time) (time.Duration, string) {
	exitCh := make(chan error, 1)
	go func() { exitCh <- cmd.Wait() }()

	watchCtx, cancelWatch := context.WithCancel(ctx)
	defer cancelWatch()
	stallCh := make(chan struct{}, 1)
	go w.watchStall(watchCtx, stallCh)

	select {
	case <-ctx.Done():
		w.gracefulStop(cmd, stdin, exitCh)
		return time.Since(startedAt), ""

	case <-stallCh:
		w.logger.Warn(w.ch.Name, "sem novo .ts recente, stream travado — reiniciando", "stall_timeout_s", w.cfg.StallTimeoutSeconds)
		_ = cmd.Process.Kill()
		<-exitCh
		end := time.Now()
		w.gaps.Record(w.ch.Name, startedAt, end, "stall")
		return end.Sub(startedAt), "stall (sem novo .ts por mais de " + fmt.Sprint(w.cfg.StallTimeoutSeconds) + "s)"

	case err := <-exitCh:
		end := time.Now()
		reason := fmt.Sprintf("process_exit code=%d", exitCodeOf(err))
		w.logger.Warn(w.ch.Name, "processo ffmpeg terminou", "code", exitCodeOf(err), "ran_s", int(end.Sub(startedAt).Seconds()))
		w.gaps.Record(w.ch.Name, startedAt, end, reason)
		return end.Sub(startedAt), reason
	}
}

// gracefulStop implementa o shutdown limpo do SPEC.md §8: envia "q" no stdin, espera
// até 10s, depois mata o processo.
func (w *Worker) gracefulStop(cmd *exec.Cmd, stdin io.WriteCloser, exitCh chan error) {
	w.logger.Info(w.ch.Name, "parando ffmpeg (shutdown limpo)")
	if _, err := io.WriteString(stdin, "q"); err != nil {
		w.logger.Warn(w.ch.Name, "falha ao escrever 'q' no stdin do ffmpeg", "err", err)
	}
	_ = stdin.Close()

	select {
	case <-exitCh:
	case <-time.After(gracefulStopTimeout):
		w.logger.Warn(w.ch.Name, "ffmpeg não encerrou a tempo, matando processo")
		_ = cmd.Process.Kill()
		<-exitCh
	}
}

// watchStall monitora work\<Canal>\ e sinaliza stallCh se nenhum .ts novo aparecer
// por mais de stall_timeout_seconds (SPEC.md §8 tabela, linha 2).
func (w *Worker) watchStall(ctx context.Context, stallCh chan<- struct{}) {
	stallTimeout := time.Duration(w.cfg.StallTimeoutSeconds) * time.Second
	lastActivity := time.Now()
	lastSeen := ""

	ticker := time.NewTicker(watchdogPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			latest, err := latestTsName(w.workDir)
			if err == nil && latest != "" && latest != lastSeen {
				lastSeen = latest
				lastActivity = time.Now()
			}
			if time.Since(lastActivity) > stallTimeout {
				select {
				case stallCh <- struct{}{}:
				default:
				}
				return
			}
		}
	}
}

func latestTsName(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	latest := ""
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(e.Name()), ".ts") && e.Name() > latest {
			latest = e.Name()
		}
	}
	return latest, nil
}

// sleepCtx dorme d ou retorna false antes se ctx for cancelado.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// nextBackoff dobra o backoff, respeitando o teto (SPEC.md §8: 2s,4s,8s,16s,32s,60s).
func nextBackoff(cur, max time.Duration) time.Duration {
	next := cur * 2
	if next > max {
		next = max
	}
	return next
}

func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if ok := asExitError(err, &exitErr); ok {
		return exitErr.ExitCode()
	}
	return -1
}

func asExitError(err error, target **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError)
	if ok {
		*target = ee
	}
	return ok
}
