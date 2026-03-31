package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// TestParseSodisFileStarFormats exercises parseSodisFile with various
// real-world #STAR: line formats to confirm which ones populate the
// VizieR Star catalog entry fields.
func TestParseSodisFileStarFormats(t *testing.T) {
	cases := []struct {
		name      string
		starValue string
		wantUCAC  string
		wantTYC   string
		wantHIP   string
	}{
		// Standard two-word formats (should all work)
		{"UCAC4 standard", "UCAC4 523-052466", "523-052466", "", ""},
		{"TYC standard", "TYC 1234-5678-1", "", "1234-5678-1", ""},
		{"HIP standard", "HIP 12345", "", "", "12345"},

		// Gaia catalog (currently no field for it)
		{"Gaia DR3", "Gaia DR3 1234567890", "", "", ""},
		{"Gaia3 format", "Gaia3 1234567890", "", "", ""},

		// Numeric prefix catalogs (OWCloud format) — should now match via Contains
		{"4Ucac2 prefix", "4Ucac2 28863823", "28863823", "", ""},
		{"2UCAC4 prefix", "2UCAC4 523-052466", "523-052466", "", ""},

		// Single word (no space between catalog and ID)
		{"UCAC4-no-space", "UCAC4-523-052466", "", "", ""},
		{"TYC-no-space", "TYC1234-5678-1", "", "", ""},

		// Empty and unknown
		{"empty", "", "", "", ""},
		{"unknown", "unknown", "", "", ""},

		// Extra fields after catalog+ID
		{"UCAC4 with magnitude", "UCAC4 523-052466 V=14.5", "523-052466", "", ""},
	}

	_ = test.NewApp()

	tmpDir := t.TempDir()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Write a minimal SODIS report file
			content := fmt.Sprintf("#STAR:  %s\n", tc.starValue)
			path := filepath.Join(tmpDir, "test_sodis.txt")
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				t.Fatal(err)
			}

			// Create a VizieRTab with real entries
			vt := &VizieRTab{
				DateYearEntry:       NewFocusLossEntry(),
				DateMonthEntry:      NewFocusLossEntry(),
				DateDayEntry:        NewFocusLossEntry(),
				StarUCAC4Entry:      NewFocusLossEntry(),
				StarTycho2Entry:     NewFocusLossEntry(),
				StarHipparcosEntry:  NewFocusLossEntry(),
				SiteLongDegEntry:    NewFocusLossEntry(),
				SiteLongMinEntry:    NewFocusLossEntry(),
				SiteLongSecsEntry:   NewFocusLossEntry(),
				SiteLatDegEntry:     NewFocusLossEntry(),
				SiteLatMinEntry:     NewFocusLossEntry(),
				SiteLatSecsEntry:    NewFocusLossEntry(),
				SiteAltitudeEntry:   NewFocusLossEntry(),
				ObserverNameEntry:   NewFocusLossEntry(),
				AsteroidNumberEntry: NewFocusLossEntry(),
				AsteroidNameEntry:   NewFocusLossEntry(),
				StatusLabel:         nil,
			}

			// parseSodisFile calls vt.StatusLabel.SetText — stub it
			// Actually, StatusLabel.SetText will panic on nil.
			// We need a real label.
			vt.StatusLabel = widget.NewLabel("")

			if err := vt.parseSodisFile(path, nil); err != nil {
				t.Fatalf("parseSodisFile: %v", err)
			}

			if got := vt.StarUCAC4Entry.Text; got != tc.wantUCAC {
				t.Errorf("UCAC4: got %q, want %q", got, tc.wantUCAC)
			}
			if got := vt.StarTycho2Entry.Text; got != tc.wantTYC {
				t.Errorf("Tycho2: got %q, want %q", got, tc.wantTYC)
			}
			if got := vt.StarHipparcosEntry.Text; got != tc.wantHIP {
				t.Errorf("Hipparcos: got %q, want %q", got, tc.wantHIP)
			}
		})
	}
}
