package main

import (
	"math"
	"os"
	"testing"
	"time"
)

// TestGetTforMinimumDistance exercises the full pipeline (XML parse → Newton
// solver → geoid lookup) against the inline sample event at four reference
// sites. Output is emitted with t.Logf; run with `go test -v -run
// TestGetTforMinimumDistance` to view the per-site timing table.
func TestGetTforMinimumDistance(t *testing.T) {
	tmp, err := os.CreateTemp("", "occelmnt-*.xml")
	if err != nil {
		t.Fatalf("tempfile: %v", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write([]byte(sampleOccelmntXML)); err != nil {
		t.Fatalf("write tempfile: %v", err)
	}
	tmp.Close()

	events, err := ParseOccelmntXML(tmp.Name())
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

func clampUnit(x float64) float64 {
	if x < -1 {
		return -1
	}
	if x > 1 {
		return 1
	}
	return x
}
