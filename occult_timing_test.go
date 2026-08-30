package main

import (
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

// TestGetTforMinimumDistance exercises the full pipeline (XML parse → Newton
// solver → geoid lookup) against the inline sample event at four reference
// sites. Output is emitted with t.Logf; run with `go test -v -run
// TestGetTforMinimumDistance` to view the per-site timing table.
func TestGetTforMinimumDistance(t *testing.T) {
	events, err := ParseOccelmntXML(writeTempXML(t, sampleOccelmntXML))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("no events parsed from sample XML")
	}

	type site struct {
		name string
		lon  float64
		lat  float64
		alt  float64
	}
	sites := []site{
		{"Portland, OR", -122.6784, 45.5152, 15.0},
		{"Honolulu, HI", -157.8583, 21.3069, 10.0},
		{"Greenwich, UK", -0.0014, 51.4779, 46.0},
		{"Sydney, AU", 151.2093, -33.8688, 58.0},
	}

	for idx, occ := range events {
		t.Logf("=== Event %d ===", idx+1)
		t.Logf("Geocentric mid-time: %s", occ.EventDateTime.Format(time.RFC3339Nano))
		t.Logf("Apparent FPlaneDec: %.6f deg", occ.FPlaneDecDeg)
		t.Logf("Apparent SubStellarLong: %.6f deg", occ.SubStellarLong)

		t.Logf("%-18s %10s %22s %12s %8s %8s %8s",
			"Site", "Ta (hr)", "Event UTC", "MinDist km", "GeoidN m", "StarAlt", "SunAlt")
		for _, s := range sites {
			N := GeoidHeight(s.lon, s.lat)
			ellipsoidalAltM := s.alt + N

			r := GetTforMinimumDistance(occ, s.lon, s.lat, ellipsoidalAltM, 0.0, 0.0)
			if !r.Converged {
				t.Errorf("site %s: Newton solver did not converge after %d iterations", s.name, r.Iterations)
			}
			starAlt := math.Asin(clampUnit(r.Z)) * radian
			sunAlt := math.Asin(clampUnit(r.ZSun)) * radian
			t.Logf("%-18s %10.5f %22s %12.1f %8.2f %8.2f %8.2f",
				s.name, r.Ta,
				r.EventTimeUTC.Format("2006-01-02 15:04:05"),
				r.MinDistanceKm, N, starAlt, sunAlt)
		}
	}
}

const sampleOccelmntXML = `<Occultations>
  <Event>
    <Elements>JPL#58:2025-11-27@2026-03-11[OWC],0.29,2026,3,11,20.3240273,-0.1755468,0.3008027,5.8328637,3.4196768,0.0056381,0.0002877,0.0000003,-0.0000005</Elements>
    <Earth>-20.0514,3.4295,-122.38,-3.46,False</Earth>
    <Star>J061552.44+032622.3,6.26456690,3.4395323,8.47,8.46,8.41,0.0,0,,6.28772718,3.4294840,10.39,9.54,0,0,0</Star>
    <Object>81879,2000 LB11,18.84,3.500,2.2622,0,0,1.515,13.29,,0.6,0,18.85,17.95,</Object>
    <Orbit>0,54.1317,2026,3,11,196.1475,218.7943,17.2293,0.07937,2.77654,2.55617,13.90,5.0,0.15</Orbit>
    <Errors>1.765,0.0024,0.0003,102,0.0024,Known errors,0.95,1,-1,-1</Errors>
    <ID>20260311_2622.3,61100.81</ID>
  </Event>
</Occultations>`

// writeTempXML writes contents to a temporary .xml file and returns its path.
// The file is removed when the test (or subtest) finishes. Close is handled
// explicitly rather than deferred: a close failure means the contents may not
// have reached disk, so parsing the file would exercise the wrong bytes.
func writeTempXML(t *testing.T, contents string) string {
	t.Helper()

	tmp, err := os.CreateTemp("", "occelmnt-*.xml")
	if err != nil {
		t.Fatalf("tempfile: %v", err)
	}
	path := tmp.Name()
	t.Cleanup(func() {
		if rerr := os.Remove(path); rerr != nil && !os.IsNotExist(rerr) {
			t.Errorf("remove tempfile %s: %v", path, rerr)
		}
	})

	if _, werr := tmp.Write([]byte(contents)); werr != nil {
		if cerr := tmp.Close(); cerr != nil {
			t.Errorf("close tempfile %s: %v", path, cerr)
		}
		t.Fatalf("write tempfile %s: %v", path, werr)
	}
	if cerr := tmp.Close(); cerr != nil {
		t.Fatalf("close tempfile %s: %v", path, cerr)
	}
	return path
}

func clampUnit(x float64) float64 {
	if x < -1 {
		return -1
	}
	if x > 1 {
		return 1
	}
	return x
}

// TestComputeMagDrop checks the star+asteroid flux-combining math against
// three reference cases: equal magnitudes (50% drop, Δm = 2.5·log10(2)),
// the sample-event values (V_star = 8.46, V_asteroid = 18.85 → Δm ≈ 10.39),
// and the degenerate guard (one input <= 0 returns 0).
func TestComputeMagDrop(t *testing.T) {
	cases := []struct {
		name      string
		starMag   float64
		astMag    float64
		wantDelta float64
		tol       float64
	}{
		{"equal magnitudes", 10.0, 10.0, 2.5 * math.Log10(2), 1e-9},
		{"sample event", 8.46, 18.85, 10.391, 0.005},
		{"asteroid much brighter (~0 drop)", 18.0, 8.0, 0.0001, 0.001},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputeMagDrop(tc.starMag, tc.astMag)
			if math.Abs(got-tc.wantDelta) > tc.tol {
				t.Errorf("ComputeMagDrop(%v, %v) = %v, want %v (±%v)",
					tc.starMag, tc.astMag, got, tc.wantDelta, tc.tol)
			}
		})
	}

	if got := ComputeMagDrop(0, 10); got != 0 {
		t.Errorf("ComputeMagDrop with starMag=0 should return 0, got %v", got)
	}
	if got := ComputeMagDrop(10, -1); got != 0 {
		t.Errorf("ComputeMagDrop with negative asteroidMag should return 0, got %v", got)
	}
}

// TestParseStarDiamMas checks that the stellar diameter is taken from <Star>
// 1-indexed entry 7. Zero is a legitimate value there, so both the sample
// event (0.0) and a non-zero variant must come through unchanged.
func TestParseStarDiamMas(t *testing.T) {
	nonZeroXML := strings.Replace(sampleOccelmntXML,
		"8.47,8.46,8.41,0.0,0,,",
		"8.47,8.46,8.41,0.34,0,,", 1)
	if nonZeroXML == sampleOccelmntXML {
		t.Fatalf("test setup: <Star> substitution did not match the sample XML")
	}

	cases := []struct {
		name string
		xml  string
		want float64
	}{
		{"sample event (zero is usable)", sampleOccelmntXML, 0.0},
		{"non-zero diameter", nonZeroXML, 0.34},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			events, err := ParseOccelmntXML(writeTempXML(t, tc.xml))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if len(events) == 0 {
				t.Fatalf("no events parsed")
			}
			if got := events[0].StarDiamMas; got != tc.want {
				t.Errorf("StarDiamMas = %v, want %v", got, tc.want)
			}
		})
	}
}
