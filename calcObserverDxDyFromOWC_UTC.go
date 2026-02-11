package main

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// -------------------- XML schema (minimal) --------------------

type Occultations struct {
	XMLName xml.Name `xml:"Occultations"`
	Events  []Event  `xml:"Event"`
}

type Event struct {
	Elements string `xml:"Elements"`
	Star     string `xml:"Star"`
}

// -------------------- Utilities --------------------

// Used in the (unused) main()
//func must(err error) {
//	if err != nil {
//		if _, perr := fmt.Fprintln(os.Stderr, "error:", err); perr != nil {
//			panic(perr)
//		}
//		os.Exit(1)
//	}
//}

func splitCSVLoose(s string) []string {
	// Split by comma and trim whitespace/newlines/tabs around fields.
	raw := strings.Split(s, ",")
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		t := strings.TrimSpace(r)
		out = append(out, t)
	}
	return out
}

func parseFloat(field string) (float64, error) {
	field = strings.TrimSpace(field)
	if field == "" {
		return 0, errors.New("empty float field")
	}
	return strconv.ParseFloat(field, 64)
}

func deg2rad(d float64) float64 { return d * math.Pi / 180.0 }

//func rad2deg(r float64) float64 { return r * 180.0 / math.Pi }

// -------------------- Time: UTC -> Julian Date -> GMST --------------------
//
// We need Earth rotation to convert site-fixed ECEF into inertial (ECI) for dotting
// against the star-based fundamental-plane basis.
// This GMST formula is standard and accurate enough for occultation geometry
// at the ~sub-arcsecond level (ignores UT1-UTC, polar motion, etc.).

func julianDateUTC(t time.Time) float64 {
	// Valid for Gregorian calendar dates (modern dates).
	utc := t.UTC()
	y := utc.Year()
	m := int(utc.Month())
	d := utc.Day()

	h := float64(utc.Hour())
	minute := float64(utc.Minute())
	sec := float64(utc.Second()) + float64(utc.Nanosecond())*1e-9
	dayFrac := (h + minute/60.0 + sec/3600.0) / 24.0

	if m <= 2 {
		y--
		m += 12
	}
	A := y / 100
	B := 2 - A + (A / 4)

	// JD at 0h UT
	jd0 := math.Floor(365.25*float64(y+4716)) +
		math.Floor(30.6001*float64(m+1)) +
		float64(d) + float64(B) - 1524.5

	return jd0 + dayFrac
}

func gmstRadians(t time.Time) float64 {
	// IAU-style approximation:
	// GMST (deg) = 280.46061837 + 360.98564736629*(JD-2451545.0)
	//             + 0.000387933*T^2 - T^3/38710000
	// where T = (JD-2451545.0)/36525
	jd := julianDateUTC(t)
	T := (jd - 2451545.0) / 36525.0
	gmstDeg := 280.46061837 +
		360.98564736629*(jd-2451545.0) +
		0.000387933*T*T -
		(T*T*T)/38710000.0

	// Normalize to [0, 360)
	gmstDeg = math.Mod(gmstDeg, 360.0)
	if gmstDeg < 0 {
		gmstDeg += 360.0
	}
	return deg2rad(gmstDeg)
}

// -------------------- Geodesy: WGS84 lat/lon/h -> ECEF --------------------

type Vec3 struct{ x, y, z float64 }

func (v Vec3) dot(u Vec3) float64 { return v.x*u.x + v.y*u.y + v.z*u.z }

func ecefFromGeodetic(latRad, lonRad, hKm float64) Vec3 {
	// WGS84
	const a = 6378.137            // km
	const f = 1.0 / 298.257223563 // flattening
	const e2 = f * (2.0 - f)      // eccentricity^2
	sinφ := math.Sin(latRad)
	cosφ := math.Cos(latRad)
	sinλ := math.Sin(lonRad)
	cosλ := math.Cos(lonRad)

	N := a / math.Sqrt(1.0-e2*sinφ*sinφ)

	x := (N + hKm) * cosφ * cosλ
	y := (N + hKm) * cosφ * sinλ
	z := (N*(1.0-e2) + hKm) * sinφ
	return Vec3{x, y, z}
}

func rotZ(thetaRad float64, v Vec3) Vec3 {
	c := math.Cos(thetaRad)
	s := math.Sin(thetaRad)
	return Vec3{
		x: c*v.x - s*v.y,
		y: s*v.x + c*v.y,
		z: v.z,
	}
}

// -------------------- Fundamental plane basis from star RA/Dec --------------------

func basisXiEta(alphaRad, deltaRad float64) (xi, eta Vec3) {
	// In an inertial frame (ECI):
	// xi-hat = (-sinα, cosα, 0)
	// eta-hat = (-sinδ cosα, -sinδ sinα, cosδ)
	sinα := math.Sin(alphaRad)
	cosα := math.Cos(alphaRad)
	sinδ := math.Sin(deltaRad)
	cosδ := math.Cos(deltaRad)

	xi = Vec3{-sinα, cosα, 0}
	eta = Vec3{-sinδ * cosα, -sinδ * sinα, cosδ}
	return
}

// -------------------- Parse OWC fields --------------------

type ParsedElements struct {
	tag            string
	durationSec    float64
	year, mon, day int
	utcHours       float64
	X0, Y0         float64 // Earth radii
	// velocities and higher terms exist but aren't needed for dX, dY at t0
}

func parseElements(elementsText string) (*ParsedElements, error) {
	f := splitCSVLoose(elementsText)
	// Expected ordering from your file:
	// 0 tag
	// 1 duration
	// 2 year
	// 3 month
	// 4 day
	// 5 utcHours
	// 6 X0
	// 7 Y0
	// 8 dX/dt
	// 9 dY/dt
	// 10 d
	// 11 X2
	// 12 Y2
	// 13 correction
	if len(f) < 8 {
		return nil, fmt.Errorf("elements has too few fields: got %d", len(f))
	}

	pe := &ParsedElements{}
	pe.tag = f[0]

	var err error
	pe.durationSec, err = parseFloat(f[1])
	if err != nil {
		return nil, fmt.Errorf("duration: %w", err)
	}

	yi, err := strconv.Atoi(strings.TrimSpace(f[2]))
	if err != nil {
		return nil, fmt.Errorf("year: %w", err)
	}
	mi, err := strconv.Atoi(strings.TrimSpace(f[3]))
	if err != nil {
		return nil, fmt.Errorf("month: %w", err)
	}
	di, err := strconv.Atoi(strings.TrimSpace(f[4]))
	if err != nil {
		return nil, fmt.Errorf("day: %w", err)
	}
	pe.year, pe.mon, pe.day = yi, mi, di

	pe.utcHours, err = parseFloat(f[5])
	if err != nil {
		return nil, fmt.Errorf("utcHours: %w", err)
	}

	pe.X0, err = parseFloat(f[6])
	if err != nil {
		return nil, fmt.Errorf("x0: %w", err)
	}
	pe.Y0, err = parseFloat(f[7])
	if err != nil {
		return nil, fmt.Errorf("y0: %w", err)
	}

	return pe, nil
}

type ParsedStar struct {
	id      string
	raHours float64
	decDeg  float64
	// Some OWC lines also include "apparent RA/Dec" later; we optionally use it if present.
	raHoursApp float64
	decDegApp  float64
	hasApp     bool
}

func parseStar(starText string) (*ParsedStar, error) {
	f := splitCSVLoose(starText)
	// Based on your sample:
	// 0 id
	// 1 RA(hours)
	// 2 Dec(deg)
	// ...
	// 9 RA_app(hours)   (because there's an empty field after PM terms)
	// 10 Dec_app(deg)
	if len(f) < 3 {
		return nil, fmt.Errorf("star has too few fields: got %d", len(f))
	}
	ps := &ParsedStar{id: f[0]}
	var err error
	ps.raHours, err = parseFloat(f[1])
	if err != nil {
		return nil, fmt.Errorf("RA(hours): %w", err)
	}
	ps.decDeg, err = parseFloat(f[2])
	if err != nil {
		return nil, fmt.Errorf("dec(deg): %w", err)
	}

	// Try to read the apparent RA/Dec if present and parseable.
	// In your example they appear as fields 9 and 10 (0-based).
	if len(f) >= 11 {
		raApp, err1 := strconv.ParseFloat(strings.TrimSpace(f[9]), 64)
		decApp, err2 := strconv.ParseFloat(strings.TrimSpace(f[10]), 64)
		if err1 == nil && err2 == nil && f[9] != "" && f[10] != "" {
			ps.raHoursApp = raApp
			ps.decDegApp = decApp
			ps.hasApp = true
		}
	}
	return ps, nil
}

func timeFromYMDHoursUTC(y, m, d int, utcHours float64) (time.Time, error) {
	if utcHours < 0 || utcHours >= 24 {
		return time.Time{}, fmt.Errorf("utcHours out of range: %v", utcHours)
	}
	h := int(math.Floor(utcHours))
	rem := (utcHours - float64(h)) * 60.0
	minute := int(math.Floor(rem))
	sec := (rem - float64(minute)) * 60.0

	s := int(math.Floor(sec))
	ns := int(math.Round((sec - float64(s)) * 1e9))
	if ns == 1_000_000_000 {
		ns = 0
		s++
	}
	return time.Date(y, time.Month(m), d, h, minute, s, ns, time.UTC), nil
}

// -------------------- Core computation --------------------

type Result struct {
	t0UTC      time.Time
	X0, Y0     float64
	xObs, yObs float64
	dX, dY     float64
}

func computeDXDYAtT0(xmlPath string, latDeg, lonDeg, altMeters float64) (*Result, error) {
	f, err := os.Open(xmlPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			if _, perr := fmt.Fprintf(os.Stderr, "error closing XML file: %v\n", cerr); perr != nil {
				panic(perr)
			}
		}
	}()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	var occ Occultations
	if err := xml.Unmarshal(data, &occ); err != nil {
		return nil, err
	}
	if len(occ.Events) == 0 {
		return nil, errors.New("no <Event> found")
	}
	ev := occ.Events[0]

	pe, err := parseElements(ev.Elements)
	if err != nil {
		return nil, err
	}

	ps, err := parseStar(ev.Star)
	if err != nil {
		return nil, err
	}

	t0, err := timeFromYMDHoursUTC(pe.year, pe.mon, pe.day, pe.utcHours)
	if err != nil {
		return nil, err
	}

	// Choose apparent RA/Dec if present; otherwise base RA/Dec.
	raHours := ps.raHours
	decDeg := ps.decDeg
	if ps.hasApp {
		raHours = ps.raHoursApp
		decDeg = ps.decDegApp
	}

	alpha := deg2rad(15.0 * raHours) // hours -> degrees -> rad
	delta := deg2rad(decDeg)

	// Observer geodetic -> ECEF (km)
	lat := deg2rad(latDeg)
	lon := deg2rad(lonDeg) // assume East-positive degrees
	hKm := altMeters / 1000.0
	rECEF := ecefFromGeodetic(lat, lon, hKm)

	// ECEF -> ECI using GMST rotation
	theta := gmstRadians(t0)
	rECI := rotZ(theta, rECEF)

	// Fundamental-plane basis from star RA/Dec (ECI)
	xi, eta := basisXiEta(alpha, delta)

	// Project observer onto (xi, eta) and convert km -> Earth radii
	const a = 6378.137
	xObs := (rECI.dot(xi)) / a
	yObs := (rECI.dot(eta)) / a

	// dX, dY as shadow axis minus observer projection at t0
	dX := pe.X0 - xObs
	dY := pe.Y0 - yObs

	return &Result{
		t0UTC: t0,
		X0:    pe.X0, Y0: pe.Y0,
		xObs: xObs, yObs: yObs,
		dX: dX, dY: dY,
	}, nil
}

// -------------------- CLI --------------------

//func main() {
//	var (
//		inFile = flag.String("in", "", "Path to OWC occelmnt XML file")
//		lat    = flag.Float64("lat", 0, "Observer latitude (deg, +N)")
//		lon    = flag.Float64("lon", 0, "Observer longitude (deg, +E; use negative for West)")
//		alt    = flag.Float64("alt", 0, "Observer altitude (meters)")
//	)
//	flag.Parse()
//
//	if *inFile == "" {
//		if _, perr := fmt.Fprintln(os.Stderr, "usage: go run . -in event.xml -lat 34.05 -lon -118.25 -alt 200"); perr != nil {
//			panic(perr)
//		}
//		os.Exit(2)
//	}
//
//	res, err := computeDXDYAtT0(*inFile, *lat, *lon, *alt)
//	must(err)
//
//	fmt.Printf("t0 (UTC): %s\n", res.t0UTC.Format(time.RFC3339Nano))
//	fmt.Printf("X0, Y0 (Earth radii): %.10f, %.10f\n", res.X0, res.Y0)
//	fmt.Printf("x_obs, y_obs (Earth radii): %.10f, %.10f\n", res.xObs, res.yObs)
//	fmt.Printf("dX, dY = (X0-x_obs, Y0-y_obs) [Earth radii]: %.10f, %.10f\n", res.dX, res.dY)
//
//	// Helpful: convert dX and dY to km for intuition
//	const a = 6378.137
//	fmt.Printf("dX, dY in km: %.3f, %.3f\n", res.dX*a, res.dY*a)
//	fmt.Printf("impact parameter b = sqrt(dX^2 + dY^2): %.10f Re  (%.3f km)\n",
//		math.Hypot(res.dX, res.dY), math.Hypot(res.dX, res.dY)*a)
//
//	fmt.Println("\nNOTE:")
//	fmt.Println(" - This includes latitude/longitude/altitude via WGS-84 + Earth rotation (GMST).")
//	fmt.Println(" - It does NOT include UT1-UTC, polar motion, precession/nutation, or topocentric star aberration.")
//	fmt.Println("   For sub-10 ms work, you’ll want UT1 and a full IERS rotation model.")
//}
