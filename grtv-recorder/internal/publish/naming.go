// Package publish implementa a publicação: varredura de work\<Canal>\, pipeline de
// validação/remux/thumbnail e nomeação de arquivo (SPEC.md §7).
package publish

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"
)

// tsNameLayout casa com o -strftime do ffmpeg: "%Y-%m-%d_%H.%M.%S" (SPEC.md §5.1/§5.3).
const tsNameLayout = "2006-01-02_15.04.05"

// ParseStart extrai o horário de início a partir do nome do .ts, ex.:
// "2026-09-18_00.00.00.ts" -> 2026-09-18 00:00:00 (hora local).
func ParseStart(tsPath string) (time.Time, error) {
	base := filepath.Base(tsPath)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	t, err := time.ParseInLocation(tsNameLayout, base, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("nome de .ts inválido %q: %w", filepath.Base(tsPath), err)
	}
	return t, nil
}

// nearestGridBoundary devolve o instante da grade (múltiplo de segmentSeconds a partir
// da meia-noite local) mais próximo de t.
func nearestGridBoundary(t time.Time, segmentSeconds int) time.Time {
	midnight := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	secs := t.Sub(midnight).Seconds()
	step := float64(segmentSeconds)
	nearest := math.Round(secs/step) * step
	return midnight.Add(time.Duration(nearest) * time.Second)
}

// ComputeEnd calcula o horário de fim do bloco (SPEC.md §7.2 passo 5 / §7.3):
//
//	fim = inicio + round(duration)
//	se |fim - borda_da_grade_mais_proxima| <= gridSnapSeconds: fim = borda_da_grade
func ComputeEnd(start time.Time, durationSeconds float64, segmentSeconds, gridSnapSeconds int) time.Time {
	end := start.Add(time.Duration(math.Round(durationSeconds)) * time.Second)

	boundary := nearestGridBoundary(end, segmentSeconds)
	tolerance := time.Duration(gridSnapSeconds) * time.Second
	if diff := end.Sub(boundary); diff <= tolerance && diff >= -tolerance {
		return boundary
	}
	return end
}

// FormatRange formata o nome do arquivo publicado: "HH.MM.SS-HH.MM.SS" (SPEC.md §1.1, §7.2).
func FormatRange(start, end time.Time) string {
	return fmt.Sprintf("%s-%s", start.Format("15.04.05"), end.Format("15.04.05"))
}

// DestDateDir é a pasta de data de destino, determinada exclusivamente pelo horário de
// INÍCIO do segmento (SPEC.md §6 "Regra de destino") — ex.: "2026-09-18".
func DestDateDir(start time.Time) string {
	return start.Format("2006-01-02")
}
