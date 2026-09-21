package publish

import "testing"

func TestBuildRemuxArgs_NoOffset(t *testing.T) {
	got := buildRemuxArgs("in.ts", "out.mp4", 0)
	want := []string{
		"-nostdin", "-y", "-hide_banner", "-loglevel", "error",
		"-i", "in.ts",
		"-map", "0:v:0", "-map", "0:a:0?",
		"-c", "copy", "-movflags", "+faststart",
		"out.mp4",
	}
	assertArgsEqual(t, got, want)
}

func TestBuildRemuxArgs_WithOffset_ReadsInputTwice(t *testing.T) {
	// Áudio adiantado 1.5s na fonte (caso de campo, BOV02): atrasa só o áudio via
	// -itsoffset, lendo o mesmo .ts local duas vezes, sem reencodar nada.
	got := buildRemuxArgs("in.ts", "out.mp4", 1.5)
	want := []string{
		"-nostdin", "-y", "-hide_banner", "-loglevel", "error",
		"-i", "in.ts",
		"-itsoffset", "1.500",
		"-i", "in.ts",
		"-map", "0:v:0", "-map", "1:a:0?",
		"-c", "copy", "-movflags", "+faststart",
		"out.mp4",
	}
	assertArgsEqual(t, got, want)
}

func assertArgsEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d args, want %d\ngot:  %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("arg[%d]: got %q, want %q\ngot:  %v\nwant: %v", i, got[i], want[i], got, want)
		}
	}
}
