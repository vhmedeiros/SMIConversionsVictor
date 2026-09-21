package publish

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"grtv-recorder/internal/config"
	"grtv-recorder/internal/fsutil"
	"grtv-recorder/internal/logging"
)

// Publisher varre work\<Canal>\, detecta .ts fechados e roda o pipeline de
// publicação (SPEC.md §7). Uma instância por canal.
type Publisher struct {
	channel       string
	workDir       string
	quarantineDir string
	outputDir     string

	ffmpegPath  string
	ffprobePath string

	segmentSeconds       int
	gridSnapSeconds      int
	tickSeconds          int
	orphanTimeoutSeconds int
	maxRetries           int
	avSyncOffset         float64

	logger   *logging.Logger
	attempts map[string]int
}

// New cria o publisher de um canal a partir da config global.
func New(cfg *config.Config, channelName string, logger *logging.Logger) *Publisher {
	var avSyncOffset float64
	for _, ch := range cfg.Channels {
		if ch.Name == channelName {
			avSyncOffset = ch.AVSyncOffsetSeconds
			break
		}
	}

	return &Publisher{
		channel:              channelName,
		workDir:              cfg.ChannelWorkDir(channelName),
		quarantineDir:        cfg.QuarantineDir(),
		outputDir:            cfg.OutputDir,
		ffmpegPath:           cfg.FfmpegPath,
		ffprobePath:          cfg.FfprobePath,
		segmentSeconds:       cfg.SegmentSeconds,
		gridSnapSeconds:      cfg.GridSnapSeconds,
		tickSeconds:          cfg.PublishTickSeconds,
		orphanTimeoutSeconds: cfg.OrphanTimeoutSeconds,
		maxRetries:           cfg.MaxPublishRetries,
		avSyncOffset:         avSyncOffset,
		logger:               logger,
		attempts:             make(map[string]int),
	}
}

// Run é o loop principal: ticker de publish_tick_seconds varrendo work\<Canal>\ (SPEC.md §7).
func (p *Publisher) Run(ctx context.Context) {
	if err := fsutil.MkdirAll(p.workDir); err != nil {
		p.logger.Error(p.channel, "falha ao criar pasta de trabalho do canal", "err", err)
	}

	ticker := time.NewTicker(time.Duration(p.tickSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.scanOnce()
		}
	}
}

// PublishAll publica TODOS os .ts da pasta de trabalho, sem aplicar a heurística de
// "fechado" (SPEC.md §7.1) — usado apenas no shutdown limpo, quando o processo ffmpeg
// do canal já terminou e portanto todo .ts restante está garantidamente fechado
// (SPEC.md §8: "Publica o que estiver fechado antes de sair").
func (p *Publisher) PublishAll() {
	entries, err := os.ReadDir(p.workDir)
	if err != nil {
		if !os.IsNotExist(err) {
			p.logger.Error(p.channel, "falha ao ler pasta de trabalho no flush final", "err", err)
		}
		return
	}

	var tsFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".ts") {
			tsFiles = append(tsFiles, e.Name())
		}
	}
	sort.Strings(tsFiles)

	for _, name := range tsFiles {
		p.processFile(filepath.Join(p.workDir, name))
	}
}

// scanOnce varre work\<Canal>\*.ts e publica os fechados (SPEC.md §7.1).
func (p *Publisher) scanOnce() {
	entries, err := os.ReadDir(p.workDir)
	if err != nil {
		if !os.IsNotExist(err) {
			p.logger.Error(p.channel, "falha ao ler pasta de trabalho", "err", err)
		}
		return
	}

	var tsFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(e.Name()), ".ts") {
			tsFiles = append(tsFiles, e.Name())
		}
	}
	// Nomes gerados por -strftime ordenam cronologicamente como string.
	sort.Strings(tsFiles)

	for i, name := range tsFiles {
		full := filepath.Join(p.workDir, name)

		closed := false
		if i < len(tsFiles)-1 {
			// Existe um .ts mais recente: o ffmpeg já fechou este (caso normal).
			closed = true
		} else {
			info, err := os.Stat(full)
			if err != nil {
				continue
			}
			if time.Since(info.ModTime()) > time.Duration(p.orphanTimeoutSeconds)*time.Second {
				// mtime parado: órfão de crash/reboot (SPEC.md §7.1).
				closed = true
			}
		}

		if closed {
			p.processFile(full)
		}
	}
}

// processFile roda o pipeline completo de um .ts fechado (SPEC.md §7.2).
func (p *Publisher) processFile(tsPath string) {
	ctx := context.Background()
	name := filepath.Base(tsPath)

	fail := func(stage string, err error) {
		p.attempts[name]++
		p.logger.Warn(p.channel, "falha ao publicar, será retentado", "file", name, "stage", stage, "attempt", p.attempts[name], "err", err)
		if p.attempts[name] >= p.maxRetries {
			p.quarantine(tsPath, stage, err)
		}
	}

	// 1. VALIDAR
	tsDur, err := Duration(ctx, p.ffprobePath, tsPath)
	if err != nil || tsDur < 1.0 {
		if err == nil {
			err = fmt.Errorf("duração %.3fs < 1.0s", tsDur)
		}
		p.logger.Warn(p.channel, "arquivo inválido, quarentena", "file", name, "err", err)
		p.quarantine(tsPath, "validate", err)
		return
	}

	stem := strings.TrimSuffix(tsPath, filepath.Ext(tsPath))
	mp4Path := stem + ".mp4"
	jpgPath := stem + ".jpg"

	// 2. REMUX
	if err := p.remux(ctx, tsPath, mp4Path); err != nil {
		fail("remux", err)
		return
	}

	// 3. DURAÇÃO REAL
	realDur, err := Duration(ctx, p.ffprobePath, mp4Path)
	if err != nil {
		fail("probe_mp4", err)
		return
	}

	// 4. THUMBNAIL (do .mp4 final)
	if err := p.thumbnail(ctx, mp4Path, jpgPath); err != nil {
		fail("thumbnail", err)
		return
	}

	// 5. NOMEAR
	start, err := ParseStart(tsPath)
	if err != nil {
		fail("parse_start", err)
		return
	}
	end := ComputeEnd(start, realDur, p.segmentSeconds, p.gridSnapSeconds)
	// Encosta o início na grade para o NOME do arquivo (evita o "buraco" cosmético de
	// 1-2s do jitter de GOP do RTSP entre o fim de um arquivo e o início do próximo —
	// ver SnapToGrid). A pasta de destino continua usando o horário cru (abaixo).
	displayStart := SnapToGrid(start, p.segmentSeconds, p.gridSnapSeconds)
	rangeName := FormatRange(displayStart, end)

	// 6. DESTINO — sempre pelo horário de início CRU, nunca o ajustado (SPEC.md §6).
	destDir := filepath.Join(p.outputDir, DestDateDir(start), p.channel)
	if err := fsutil.MkdirAll(destDir); err != nil {
		fail("mkdir_dest", err)
		return
	}

	base, err := uniquePairBase(destDir, rangeName)
	if err != nil {
		fail("unique_dest", err)
		return
	}
	dstMp4 := filepath.Join(destDir, base+".mp4")
	dstJpg := filepath.Join(destDir, base+".jpg")

	// 7. MOVER (ordem obrigatória: JPG primeiro)
	if err := fsutil.Rename(jpgPath, dstJpg); err != nil {
		fail("move_jpg", err)
		return
	}
	if err := fsutil.Rename(mp4Path, dstMp4); err != nil {
		fail("move_mp4", err)
		return
	}

	// 8. LIMPAR
	if err := os.Remove(tsPath); err != nil {
		p.logger.Warn(p.channel, "falha ao remover .ts publicado", "file", name, "err", err)
	}

	delete(p.attempts, name)
	p.logger.Info(p.channel, "published", "file", base+".mp4", "dur", fmt.Sprintf("%.1fs", realDur))
}

// remux converte .ts em .mp4 sem reencode (SPEC.md §7.2 passo 2).
func (p *Publisher) remux(ctx context.Context, tsPath, mp4Path string) error {
	cmd := exec.CommandContext(ctx, p.ffmpegPath, buildRemuxArgs(tsPath, mp4Path, p.avSyncOffset)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg remux: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// buildRemuxArgs monta os argumentos do remux .ts -> .mp4 (SPEC.md §7.2 passo 2).
//
// Com avSyncOffset == 0 (caso normal), lê o .ts uma vez, igual sempre foi.
//
// Com avSyncOffset != 0, lê o MESMO .ts local duas vezes: uma para o vídeo sem tocar
// em nada, outra só para o áudio com -itsoffset aplicado — desloca o timestamp do
// áudio no tempo sem decodificar nem reencodar um único frame (corrige um offset fixo
// entre os streams já presente na fonte, medido em campo). Como é um arquivo local
// (não a captura ao vivo), ler duas vezes é barato e não arrisca a gravação. Positivo
// atrasa o áudio; negativo adianta. Continua 100% -c copy dos dois lados — não viola
// a regra do SPEC.md de nunca reencodar áudio (§5.3, critério de aceite §13.5).
func buildRemuxArgs(tsPath, mp4Path string, avSyncOffset float64) []string {
	base := []string{"-nostdin", "-y", "-hide_banner", "-loglevel", "error"}

	if avSyncOffset == 0 {
		return append(base,
			"-i", tsPath,
			"-map", "0:v:0", "-map", "0:a:0?",
			"-c", "copy", "-movflags", "+faststart",
			mp4Path,
		)
	}

	return append(base,
		"-i", tsPath,
		"-itsoffset", strconv.FormatFloat(avSyncOffset, 'f', 3, 64),
		"-i", tsPath,
		"-map", "0:v:0", "-map", "1:a:0?",
		"-c", "copy", "-movflags", "+faststart",
		mp4Path,
	)
}

// thumbnail extrai o primeiro frame do .mp4 final (SPEC.md §7.2 passo 4).
func (p *Publisher) thumbnail(ctx context.Context, mp4Path, jpgPath string) error {
	cmd := exec.CommandContext(ctx, p.ffmpegPath,
		"-nostdin", "-y", "-hide_banner", "-loglevel", "error",
		"-i", mp4Path,
		"-frames:v", "1", "-q:v", "3", "-an",
		jpgPath,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg thumbnail: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// quarantine move um .ts inválido/sem sucesso após max_publish_retries para
// work\_quarantine\ (SPEC.md §7.2, §9).
func (p *Publisher) quarantine(tsPath, stage string, cause error) {
	name := filepath.Base(tsPath)

	if err := fsutil.MkdirAll(p.quarantineDir); err != nil {
		p.logger.Error(p.channel, "falha ao criar pasta de quarentena", "err", err)
		return
	}

	dst := filepath.Join(p.quarantineDir, p.channel+"_"+name)
	unique, err := fsutil.UniqueDestination(dst)
	if err != nil {
		unique = dst
	}

	if err := os.Rename(tsPath, unique); err != nil {
		p.logger.Error(p.channel, "falha ao mover para quarentena", "file", name, "err", err)
		return
	}

	p.logger.Warn(p.channel, "arquivo movido para quarentena", "file", name, "stage", stage, "cause", cause)
	delete(p.attempts, name)
}

// uniquePairBase encontra um nome-base em destDir tal que nem "<base>.mp4" nem
// "<base>.jpg" existam, tentando sufixos "_1", "_2", ... (SPEC.md §7.2 passo 6:
// "NUNCA sobrescrever"). Mantém o par com o MESMO nome-base.
func uniquePairBase(destDir, rangeName string) (string, error) {
	exists := func(base string) (bool, error) {
		for _, ext := range []string{".mp4", ".jpg"} {
			_, err := os.Stat(filepath.Join(destDir, base+ext))
			if err == nil {
				return true, nil
			}
			if !os.IsNotExist(err) {
				return false, err
			}
		}
		return false, nil
	}

	ok, err := exists(rangeName)
	if err != nil {
		return "", err
	}
	if !ok {
		return rangeName, nil
	}

	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s_%d", rangeName, i)
		ok, err := exists(candidate)
		if err != nil {
			return "", err
		}
		if !ok {
			return candidate, nil
		}
	}
}
