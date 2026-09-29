package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBlockIntegratedCSVRoundTrip writes a block-integrated CSV and checks that reloading
// it preserves the preamble, the data, and the recorded block size.
func TestBlockIntegratedCSVRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "obs.csv")
	srcText := "# PyMovie Version 3.9.0,,\n# source: test\nFrameNo,timeInfo,signal-star\n" +
		"0,[01:02:03.0000],10\n1,[01:02:03.0400],20\n"
	if err := os.WriteFile(src, []byte(srcText), 0644); err != nil {
		t.Fatal(err)
	}
	data, err := parseLightCurveCSV(src)
	if err != nil {
		t.Fatal(err)
	}
	// Pretend these rows are the result of integrating blocks of 4 frames
	data.FrameNumbers = []float64{0, 4, 8}
	data.TimeValues = []float64{3723.0, 3723.16, 3723.32}
	data.Columns[0].Values = []float64{1.5, 0.25, 1.0}

	out, err := writeBlockIntegratedCSV(data, 4, 0)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(out) != "obs-block-integrated.csv" {
		t.Errorf("output name = %s", filepath.Base(out))
	}

	reloaded, err := parseLightCurveCSV(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.SkippedLines) != 3 || reloaded.SkippedLines[1] != "# source: test" {
		t.Errorf("preamble not preserved: %q", reloaded.SkippedLines)
	}
	if got := parseBlockIntegrationFactor(reloaded.SkippedLines); got != 4 {
		t.Errorf("block factor = %d, want 4", got)
	}
	if reloaded.HeaderLine != data.HeaderLine {
		t.Errorf("header = %q, want %q", reloaded.HeaderLine, data.HeaderLine)
	}
	for i, want := range data.TimeValues {
		if d := reloaded.TimeValues[i] - want; d > 1e-4 || d < -1e-4 {
			t.Errorf("time[%d] = %v, want %v", i, reloaded.TimeValues[i], want)
		}
		if reloaded.FrameNumbers[i] != data.FrameNumbers[i] || reloaded.Columns[0].Values[i] != data.Columns[0].Values[i] {
			t.Errorf("row %d mismatch", i)
		}
	}
	if got := analyzeTimingErrors(reloaded.TimeValues).MedianTimeStep; got < 0.1599 || got > 0.1601 {
		t.Errorf("reloaded median step = %v, want 0.16", got)
	}
}

func TestParseBlockIntegrationFactor(t *testing.T) {
	lines := []string{
		"# PyMovie",
		blockIntegratedMarker + " 1.3.7: block size = 4, first block starts at source row 2",
		blockIntegratedMarker + " 1.3.7: block size = 12, first block starts at source row 0",
	}
	if got := parseBlockIntegrationFactor(lines); got != 12 {
		t.Errorf("got %d, want 12 (last, cumulative comment)", got)
	}
	if got := parseBlockIntegrationFactor(lines[:1]); got != 1 {
		t.Errorf("got %d, want 1 with no block comment", got)
	}
}
