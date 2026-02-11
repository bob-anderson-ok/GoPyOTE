package main

//
//import (
//	"encoding/xml"
//	"errors"
//	"flag"
//	"fmt"
//	"io"
//	"math"
//	"os"
//	"strconv"
//	"strings"
//	"time"
//)
//
//// -------------------- XML schema (minimal) --------------------
//
//type Occultations struct {
//	XMLName xml.Name `xml:"Occultations"`
//	Events  []Event  `xml:"Event"`
//}
//
//type Event struct {
//	Elements string `xml:"Elements"`
//	Star     string `xml:"Star"`
//}
//
//// -------------------- Utilities --------------------
//
//func splitCSVLoose(s string) []string {
//	raw := strings.Split(s, ",")
//	out := make([]string, 0, len(raw))
//	for _, r := range raw {
//		out = append(out, strings.TrimSpace(r))
//	}
//	return out
//}
//
//func parseFloat(field string) (float64, error) {
//	field = strings.TrimSpace(field)
//	if field == "" {
//		return 0, errors.New("empty float field")
//	}
//	return strconv.ParseFloat(field, 64)
//}
//
//func deg2rad(d float64) float64 { return d * math.Pi / 180.0 }
//func arcsec2rad(a float64) float64 { return a * (math.Pi / 180.0) / 3600.0 }
//
//// -------------------- Vectors and matrices --------------------
//
//type Vec3 struct{ x, y, z float64 }
//
//func (v Vec3) dot(u Vec3) float64 { return v.x*u.x + v.y*u.y + v.z*u.z }
//
//type Mat3 [3][3]float64
//
//func (A Mat3) mulVec(v Vec3) Vec3 {
//	return Vec3{
//		x: A[0][0]*v.x + A[0][1]*v.y + A[0][2]*v.z,
//		y: A[1][0]*v.x + A[1][1]*v.y + A[1][2]*v.z,
//		z: A[2][0]*v.x + A[2][1]*v.y + A[2][2]*v.z,
//	}
//}
//
//func (A Mat3) mul(B Mat3) Mat3 {
//	var C Mat3
//	for i := 0; i < 3; i++ {
//		for j := 0; j < 3; j++ {
//			C[i][j] = A[i][0]*B[0][j] + A[i][1]*B[1][j] + A[i][2]*B[2][j]
//		}
//	}
//	return C
//}
//
//func R1(phi float64) Mat3 {
//	c, s := math.Cos(phi), math.Sin(phi)
//	return Mat3{
//		{1, 0, 0},
//		{0, c, s},
//		{0, -s, c},
//	}
//}
//func R2(phi float64) Mat3 {
//	c, s := math.Cos(phi), math.Sin(phi)
//	return Mat3{
//		{c, 0, -s},
//		{0, 1, 0},
//		{s, 0, c},
//	}
//}
//func R3(phi float64) Mat3 {
//	c, s := math.Cos(phi), math.Sin(phi)
//	return Mat3{
//		{c, s, 0},
//		{-s, c, 0},
//		{0, 0, 1},
//	}
//}
//
//// -------------------- Time: UTC -> JD(UTC), JD(UT1) --------------------
//
//func julianDateUTC(t time.Time) float64 {
//	utc := t.UTC()
//	y := int(utc.Year())
//	m := int(utc.Month())
//	d := int(utc.Day())
//
//	h := float64(utc.Hour())
//	min := float64(utc.Minute())
//	sec := float64(utc.Second()) + float64(utc.Nanosecond())*1e-9
//	dayFrac := (h + min/60.0 + sec/3600.0) / 24.0
//
//	if m <= 2 {
//		y--
//		m += 12
//	}
//	A := y / 100
//	B := 2 - A + (A / 4)
//
//	jd0 := math.Floor(365.25*float64(y+4716)) +
//		math.Floor(30.6001*float64(m+1)) +
//		float64(d) + float64(B) - 1524.5
//
//	return jd0 + dayFrac
//}
//
//func julianDateUT1(tUTC time.Time, dut1Seconds float64) float64 {
//	// JD(UT1) = JD(UTC) + DUT1/86400
//	return julianDateUTC(tUTC) + dut1Seconds/86400.0
//}
//
//// -------------------- Earth rotation: ERA (IAU 2000) --------------------
////
//// ERA = 2π * (0.7790572732640 + 1.00273781191135448 * (JD_UT1 - 2451545.0))
//// normalized to [0, 2π)
//
//func earthRotationAngleRad(jdUT1 float64) float64 {
//	d := jdUT1 - 2451545.0
//	theta := 2.0 * math.Pi * (0.7790572732640 + 1.00273781191135448*d)
//	theta = math.Mod(theta, 2.0*math.Pi)
//	if theta < 0 {
//		theta += 2.0 * math.Pi
//	}
//	return theta
//}
//
//// Optional: s' (arcseconds) approximation (IERS). It’s tiny.
//// If you don't want it, set includeSP=false.
//func sprimeRad(jdUTC float64) float64 {
//	// Common approximation: s' = -47 microarcsec * T
//	// T in Julian centuries from J2000, using UTC as a stand-in for TT here.
//	// This term is extremely small; including it is mostly for completeness.
//	T := (jdUTC - 2451545.0) / 36525.0
//	sprimeArcsec := -47e-6 * T // arcsec
//	return arcsec2rad(sprimeArcsec)
//}
//
//// -------------------- Geodesy: WGS84 lat/lon/h -> ITRF(ECEF) --------------------
//
//func ecefFromGeodetic(latRad, lonRad, hKm float64) Vec3 {
//	// WGS84
//	const a = 6378.137            // km
//	const f = 1.0 / 298.257223563
//	const e2 = f * (2.0 - f)
//
//	sinφ := math.Sin(latRad)
//	cosφ := math.Cos(latRad)
//	sinλ := math.Sin(lonRad)
//	cosλ := math.Cos(lonRad)
//
//	N := a / math.Sqrt(1.0-e2*sinφ*sinφ)
//
//	x := (N + hKm) * cosφ * cosλ
//	y := (N + hKm) * cosφ * sinλ
//	z := (N*(1.0-e2) + hKm) * sinφ
//	return Vec3{x, y, z}
//}
//
//// -------------------- Fundamental plane basis from star RA/Dec --------------------
//
//func basisXiEta(alphaRad, deltaRad float64) (xi, eta Vec3) {
//	sinα := math.Sin(alphaRad)
//	cosα := math.Cos(alphaRad)
//	sinδ := math.Sin(deltaRad)
//	cosδ := math.Cos(deltaRad)
//
//	// Inertial basis vectors
//	xi = Vec3{-sinα, cosα, 0}
//	eta = Vec3{-sinδ * cosα, -sinδ * sinα, cosδ}
//
//	_ = cosδ // keeps the structure clear; used in eta
//	return
//}
//
//// -------------------- Parse OWC fields --------------------
//
//type ParsedElements struct {
//	tag            string
//	durationSec    float64
//	year, mon, day int
//	utcHours       float64
//	X0, Y0         float64 // Earth radii
//}
//
//func parseElements(elementsText string) (*ParsedElements, error) {
//	f := splitCSVLoose(elementsText)
//	if len(f) < 8 {
//		return nil, fmt.Errorf("Elements has too few fields: got %d", len(f))
//	}
//	pe := &ParsedElements{tag: f[0]}
//
//	var err error
//	pe.durationSec, err = parseFloat(f[1])
//	if err != nil {
//		return nil, fmt.Errorf("duration: %w", err)
//	}
//	yi, err := strconv.Atoi(f[2])
//	if err != nil {
//		return nil, fmt.Errorf("year: %w", err)
//	}
//	mi, err := strconv.Atoi(f[3])
//	if err != nil {
//		return nil, fmt.Errorf("month: %w", err)
//	}
//	di, err := strconv.Atoi(f[4])
//	if err != nil {
//		return nil, fmt.Errorf("day: %w", err)
//	}
//	pe.year, pe.mon, pe.day = yi, mi, di
//
//	pe.utcHours, err = parseFloat(f[5])
//	if err != nil {
//		return nil, fmt.Errorf("utcHours: %w", err)
//	}
//
//	pe.X0, err = parseFloat(f[6])
//	if err != nil {
//		return nil, fmt.Errorf("X0: %w", err)
//	}
//	pe.Y0, err = parseFloat(f[7])
//	if err != nil {
//		return nil, fmt.Errorf("Y0: %w", err)
//	}
//	return pe, nil
//}
//
//type ParsedStar struct {
//	id        string
//	raHours   float64
//	decDeg    float64
//	raHoursApp float64
//	decDegApp  float64
//	hasApp    bool
//}
//
//func parseStar(starText string) (*ParsedStar, error) {
//	f := splitCSVLoose(starText)
//	if len(f) < 3 {
//		return nil, fmt.Errorf("Star has too few fields: got %d", len(f))
//	}
//	ps := &ParsedStar{id: f[0]}
//	var err error
//	ps.raHours, err = parseFloat(f[1])
//	if err != nil {
//		return nil, fmt.Errorf("RA(hours): %w", err)
//	}
//	ps.decDeg, err = parseFloat(f[2])
//	if err != nil {
//		return nil, fmt.Errorf("Dec(deg): %w", err)
//	}
//
//	// OWC often includes an "apparent" RA/Dec later; in your sample it's fields 9 and 10.
//	if len(f) >= 11 {
//		raApp, e1 := strconv.ParseFloat(strings.TrimSpace(f[9]), 64)
//		decApp, e2 := strconv.ParseFloat(strings.TrimSpace(f[10]), 64)
//		if e1 == nil && e2 == nil && f[9] != "" && f[10] != "" {
//			ps.raHoursApp = raApp
//			ps.decDegApp = decApp
//			ps.hasApp = true
//		}
//	}
//	return ps, nil
//}
//
//func timeFromYMDHoursUTC(y, m, d int, utcHours float64) (time.Time, error) {
//	if utcHours < 0 || utcHours >= 24 {
//		return time.Time{}, fmt.Errorf("utcHours out of range: %v", utcHours)
//	}
//	h := int(math.Floor(utcHours))
//	rem := (utcHours - float64(h)) * 60.0
//	min := int(math.Floor(rem))
//	sec := (rem - float64(min)) * 60.0
//
//	s := int(math.Floor(sec))
//	ns := int(math.Round((sec - float64(s)) * 1e9))
//	if ns == 1_000_000_000 {
//		ns = 0
//		s++
//	}
//	return time.Date(y, time.Month(m), d, h, min, s, ns, time.UTC), nil
//}
//
//// -------------------- High-precision terrestrial->celestial (UT1 + polar motion) --------------------
////
//// We treat the site vector as ITRF (ECEF). Apply polar motion W, then ERA rotation.
//// r_CIRS ≈ R3(ERA) * W * r_ITRF
////
//// W = R3(s') * R2(-xp) * R1(-yp)
////
//// Inputs:
//// - DUT1 seconds
//// - xp, yp in arcseconds
//// - includeSP: include s' tiny correction
////
//// NOTE: This does not apply precession/nutation (CIRS->GCRS). If your star RA/Dec
//// are apparent-of-date in the same intermediate frame, this is often consistent enough
//// for high-grade occultation work. If you need full rigor, add the CIP X,Y and s terms.
//
//func itrfToCirs(rITRF Vec3, tUTC time.Time, dut1Seconds, xpArcsec, ypArcsec float64, includeSP bool) Vec3 {
//	jdUTC := julianDateUTC(tUTC)
//	jdUT1 := julianDateUT1(tUTC, dut1Seconds)
//
//	era := earthRotationAngleRad(jdUT1)
//
//	xp := arcsec2rad(xpArcsec)
//	yp := arcsec2rad(ypArcsec)
//
//	sp := 0.0
//	if includeSP {
//		sp = sprimeRad(jdUTC)
//	}
//
//	W := R3(sp).mul(R2(-xp)).mul(R1(-yp)) // polar motion
//	R := R3(era)                           // Earth rotation angle
//
//	return R.mul(W).mulVec(rITRF)
//}
//
//// -------------------- Core computation --------------------
//
//type Result struct {
//	t0UTC         time.Time
//	X0, Y0        float64
//	xObs, yObs    float64
//	dX, dY        float64
//}
//
//func computeDXDYAtT0(xmlPath string, latDeg, lonDeg, altMeters float64,
//	dut1Seconds, xpArcsec, ypArcsec float64) (*Result, error) {
//
//	f, err := os.Open(xmlPath)
//	if err != nil {
//		return nil, err
//	}
//	defer f.Close()
//
//	data, err := io.ReadAll(f)
//	if err != nil {
//		return nil, err
//	}
//
//	var occ Occultations
//	if err := xml.Unmarshal(data, &occ); err != nil {
//		return nil, err
//	}
//	if len(occ.Events) == 0 {
//		return nil, errors.New("no <Event> found")
//	}
//	ev := occ.Events[0]
//
//	pe, err := parseElements(ev.Elements)
//	if err != nil {
//		return nil, err
//	}
//	ps, err := parseStar(ev.Star)
//	if err != nil {
//		return nil, err
//	}
//
//	t0, err := timeFromYMDHoursUTC(pe.year, pe.mon, pe.day, pe.utcHours)
//	if err != nil {
//		return nil, err
//	}
//
//	// Use "apparent" RA/Dec if present (often closer to what the Besselian elements assume).
//	raHours := ps.raHours
//	decDeg := ps.decDeg
//	if ps.hasApp {
//		raHours = ps.raHoursApp
//		decDeg = ps.decDegApp
//	}
//
//	alpha := deg2rad(15.0 * raHours) // hours -> degrees -> rad
//	delta := deg2rad(decDeg)
//
//	// Site: geodetic -> ITRF(ECEF) in km
//	lat := deg2rad(latDeg)
//	lon := deg2rad(lonDeg) // degrees East-positive
//	hKm := altMeters / 1000.0
//	rITRF := ecefFromGeodetic(lat, lon, hKm)
//
//	// ITRF -> (approx) CIRS via UT1 + polar motion
//	rCIRS := itrfToCirs(rITRF, t0, dut1Seconds, xpArcsec, ypArcsec, true)
//
//	// Fundamental-plane basis from star direction (assumed in same inertial-ish frame)
//	xi, eta := basisXiEta(alpha, delta)
//
//	// Project and convert km -> Earth radii
//	const Re = 6378.137
//	xObs := rCIRS.dot(xi) / Re
//	yObs := rCIRS.dot(eta) / Re
//
//	dX := pe.X0 - xObs
//	dY := pe.Y0 - yObs
//
//	return &Result{
//		t0UTC: t0,
//		X0: pe.X0, Y0: pe.Y0,
//		xObs: xObs, yObs: yObs,
//		dX: dX, dY: dY,
//	}, nil
//}
//
//// -------------------- CLI --------------------
//
//func main() {
//	var (
//		inFile = flag.String("in", "", "Path to OWC occelmnt XML file")
//		lat    = flag.Float64("lat", 0, "Observer latitude (deg, +N)")
//		lon    = flag.Float64("lon", 0, "Observer longitude (deg, +E; use negative for West)")
//		alt    = flag.Float64("alt", 0, "Observer altitude (meters)")
//
//		dut1 = flag.Float64("dut1", 0, "UT1-UTC (seconds), e.g. -0.12")
//		xp   = flag.Float64("xp", 0, "Polar motion x_p (arcseconds)")
//		yp   = flag.Float64("yp", 0, "Polar motion y_p (arcseconds)")
//	)
//	flag.Parse()
//
//	if *inFile == "" {
//		fmt.Fprintln(os.Stderr, "usage: go run . -in event.xml -lat 34.05 -lon -118.25 -alt 200 -dut1 -0.12 -xp 0.06 -yp 0.32")
//		os.Exit(2)
//	}
//
//	res, err := computeDXDYAtT0(*inFile, *lat, *lon, *alt, *dut1, *xp, *yp)
//	if err != nil {
//		fmt.Fprintln(os.Stderr, "error:", err)
//		os.Exit(1)
//	}
//
//	fmt.Printf("t0 (UTC): %s\n", res.t0UTC.Format(time.RFC3339Nano))
//	fmt.Printf("X0, Y0 (Earth radii): %.10f, %.10f\n", res.X0, res.Y0)
//	fmt.Printf("x_obs, y_obs (Earth radii): %.10f, %.10f\n", res.xObs, res.yObs)
//	fmt.Printf("dX, dY = (X0-x_obs, Y0-y_obs) [Earth radii]: %.10f, %.10f\n", res.dX, res.dY)
//
//	const Re = 6378.137
//	fmt.Printf("dX, dY in km: %.3f, %.3f\n", res.dX*Re, res.dY*Re)
//	fmt.Printf("impact parameter b = hypot(dX,dY): %.10f Re (%.3f km)\n",
//		math.Hypot(res.dX, res.dY), math.Hypot(res.dX, res.dY)*Re)
//
//	fmt.Println("\nModel notes:")
//	fmt.Println(" - Uses ERA (IAU 2000) computed from JD(UT1) with DUT1 input.")
//	fmt.Println(" - Applies polar motion x_p,y_p (IERS) and includes tiny s' term.")
//	fmt.Println(" - Does NOT apply full precession/nutation (CIRS<->GCRS).")
//	fmt.Println("   If you need full rigor, add CIP X,Y and s (from IERS) and rotate star vectors consistently.")
//}
//
