// Package service contém o orquestrador dos workers/publishers (Recorder) e o
// integração com o Service Control Manager do Windows (SPEC.md §10).
package service

import (
	"context"
	"io"
	"path/filepath"
	"sync"
	"time"

	"grtv-recorder/internal/channel"
	"grtv-recorder/internal/config"
	"grtv-recorder/internal/fsutil"
	"grtv-recorder/internal/logging"
	"grtv-recorder/internal/publish"
)

// preCreateTimeOfDay é o horário em que as pastas do novo dia são criadas
// (SPEC.md §6.1: "00:00:05").
const preCreateHour, preCreateMinute, preCreateSecond = 0, 0, 5

// statusWriteInterval é a frequência de atualização de logs\status.json.
const statusWriteInterval = 5 * time.Second

// Recorder orquestra os 8 (ou menos) workers de canal, seus publishers, a
// pré-criação de pastas de dia e o snapshot de status. É reusado tanto pelo modo
// console quanto pelo svc.Handler do Windows.
type Recorder struct {
	cfg    *config.Config
	logger *logging.Logger
	gaps   *logging.GapsLogger

	workers    []*channel.Worker
	ffmpegLogs []io.Closer
}

// NewRecorder monta um worker+publisher para cada canal habilitado.
func NewRecorder(cfg *config.Config, logger *logging.Logger, gaps *logging.GapsLogger) *Recorder {
	r := &Recorder{cfg: cfg, logger: logger, gaps: gaps}

	for _, ch := range cfg.Channels {
		if !ch.Enabled {
			continue
		}
		ffmpegLog := logging.NewFfmpegWriter(cfg.LogDir, ch.Name)
		r.ffmpegLogs = append(r.ffmpegLogs, ffmpegLog)
		r.workers = append(r.workers, channel.NewWorker(cfg, ch, logger, gaps, ffmpegLog))
	}

	return r
}

// Run bloqueia até ctx ser cancelado, supervisionando todos os canais, a
// pré-criação de pastas de dia e o snapshot de status. Ao retornar, todo
// processo ffmpeg já foi parado e o que estava fechado já foi publicado.
func (r *Recorder) Run(ctx context.Context) {
	r.logger.Info("-", "arranque do serviço", "canais", len(r.workers), "segment_seconds", r.cfg.SegmentSeconds)

	var wg sync.WaitGroup

	for _, w := range r.workers {
		wg.Add(1)
		go func(w *channel.Worker) {
			defer wg.Done()
			r.runChannel(ctx, w)
		}(w)
	}

	if r.cfg.PreCreateDayFolders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.preCreateLoop(ctx)
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		r.statusLoop(ctx)
	}()

	wg.Wait()

	for _, c := range r.ffmpegLogs {
		_ = c.Close()
	}
	r.logger.Info("-", "serviço finalizado")
}

// runChannel liga o worker (supervisor ffmpeg) e o publisher de um canal. Quando
// ctx é cancelado, espera o worker parar o ffmpeg de forma limpa e então roda um
// flush final de publicação (SPEC.md §8: "Publica o que estiver fechado antes de sair").
func (r *Recorder) runChannel(ctx context.Context, w *channel.Worker) {
	name := w.Snapshot().Name
	p := publish.New(r.cfg, name, r.logger)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		w.Run(ctx)
	}()
	go func() {
		defer wg.Done()
		p.Run(ctx)
	}()
	wg.Wait()

	p.PublishAll()
}

// preCreateLoop cria as pastas do dia corrente no arranque e, depois, todo dia às
// 00:00:05 (SPEC.md §6.1).
func (r *Recorder) preCreateLoop(ctx context.Context) {
	r.createTodayFolders()

	for {
		next := nextRunAt(time.Now(), preCreateHour, preCreateMinute, preCreateSecond)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
			r.createTodayFolders()
		}
	}
}

func nextRunAt(now time.Time, hour, min, sec int) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, min, sec, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

func (r *Recorder) createTodayFolders() {
	today := time.Now().Format("2006-01-02")
	for _, ch := range r.cfg.Channels {
		if !ch.Enabled {
			continue
		}
		dir := filepath.Join(r.cfg.OutputDir, today, ch.Name)
		if err := fsutil.MkdirAll(dir); err != nil {
			r.logger.Error(ch.Name, "falha ao pré-criar pasta do dia", "dir", dir, "err", err)
			continue
		}
		r.logger.Info(ch.Name, "criação de pasta de dia", "dir", dir)
	}
}

// statusLoop grava logs\status.json periodicamente, consumido pelo subcomando `status`.
func (r *Recorder) statusLoop(ctx context.Context) {
	ticker := time.NewTicker(statusWriteInterval)
	defer ticker.Stop()

	write := func() {
		st := Status{UpdatedAt: time.Now()}
		for _, w := range r.workers {
			snap := w.Snapshot()
			st.Channels = append(st.Channels, ChannelStatus{
				Name:      snap.Name,
				State:     string(snap.State),
				LastError: snap.LastError,
				UpdatedAt: snap.UpdatedAt,
			})
		}
		if err := WriteStatus(r.cfg.LogDir, st); err != nil {
			r.logger.Error("-", "falha ao gravar status.json", "err", err)
		}
	}

	write()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			write()
		}
	}
}
