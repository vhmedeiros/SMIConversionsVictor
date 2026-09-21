package publish

import (
	"testing"
	"time"
)

func TestParseStart(t *testing.T) {
	got, err := ParseStart("2026-09-18_00.00.00.ts")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 9, 18, 0, 0, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestComputeEnd_SnapsToGrid(t *testing.T) {
	// duração real 239.96s -> fim bruto 00.03.59 (arredondado), a 1s da borda 00.04.00 -> encosta.
	start := time.Date(2026, 9, 18, 0, 0, 0, 0, time.Local)
	end := ComputeEnd(start, 239.96, 240, 3)
	want := time.Date(2026, 9, 18, 0, 4, 0, 0, time.Local)
	if !end.Equal(want) {
		t.Fatalf("got %v, want %v", end, want)
	}
}

func TestComputeEnd_IrregularKeptAsIs(t *testing.T) {
	// stream caiu: duração real 180s, fim bruto 11.07.00, fora da tolerância de 3s da
	// borda mais próxima (11.04.00 ou 11.08.00) -> mantém irregular (SPEC.md §7.3).
	start := time.Date(2026, 9, 18, 11, 4, 0, 0, time.Local)
	end := ComputeEnd(start, 180, 240, 3)
	want := time.Date(2026, 9, 18, 11, 7, 0, 0, time.Local)
	if !end.Equal(want) {
		t.Fatalf("got %v, want %v", end, want)
	}
}

func TestComputeEnd_MidnightRollover(t *testing.T) {
	// 23:56:00 + 240s reais = 00:00:00 do dia seguinte, deve encostar exatamente.
	start := time.Date(2026, 9, 17, 23, 56, 0, 0, time.Local)
	end := ComputeEnd(start, 240, 240, 3)
	want := time.Date(2026, 9, 18, 0, 0, 0, 0, time.Local)
	if !end.Equal(want) {
		t.Fatalf("got %v, want %v", end, want)
	}
}

func TestSnapToGrid_SnapsSmallJitter(t *testing.T) {
	// Jitter de GOP do RTSP: início real 1s depois da grade (SPEC.md §5.4) — visto em
	// campo (BOV02): "07.36.01" quando o arquivo anterior já tinha fechado em "07.36.00".
	raw := time.Date(2026, 9, 21, 7, 36, 1, 0, time.Local)
	got := SnapToGrid(raw, 240, 3)
	want := time.Date(2026, 9, 21, 7, 36, 0, 0, time.Local)
	if !got.Equal(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSnapToGrid_KeepsRealGap(t *testing.T) {
	// Interrupção real de ~73s (visto em campo) não pode ser mascarada como se fosse
	// jitter — o nome deve continuar irregular (SPEC.md §7.3).
	raw := time.Date(2026, 9, 21, 5, 37, 13, 0, time.Local)
	got := SnapToGrid(raw, 240, 3)
	if !got.Equal(raw) {
		t.Fatalf("esperava que um gap real (73s) não fosse encostado na grade, got %v", got)
	}
}

func TestFormatRange(t *testing.T) {
	start := time.Date(2026, 9, 18, 23, 56, 0, 0, time.Local)
	end := time.Date(2026, 9, 19, 0, 0, 0, 0, time.Local)
	got := FormatRange(start, end)
	want := "23.56.00-00.00.00"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDestDateDir_UsesStartDate(t *testing.T) {
	// Bloco 23:56->00:00 vai para a pasta do dia que TERMINA (dia do início) — SPEC.md §6.
	start := time.Date(2026, 9, 17, 23, 56, 0, 0, time.Local)
	got := DestDateDir(start)
	want := "2026-09-17"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
