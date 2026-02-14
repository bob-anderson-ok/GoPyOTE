package main

import (
	"encoding/xml"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// ShadowVelocityFromOWCEventKmPerSec parses a single OWC-style occelmnt XML string
// and returns the relative shadow velocity (vx, vy) experienced by the observer
// on the fundamental plane, in km/s.
//
// Inputs:
//
//	xmlText      – full <Occultations> XML text (single Event expected)
//	latDeg       – observer geodetic latitude (deg)
//	lonDeg       – observer geodetic longitude (deg, east positive)
//	hMeters      – observer height above WGS84 ellipsoid (m)
//	dut1Seconds  – UT1 − UTC (seconds). If unused, pass 0.
//	xpArcsec     – polar motion x (arcsec). If unused, pass 0.
//	ypArcsec     – polar motion y (arcsec). If unused, pass 0.
//
// Assumptions:
//   - dX, dY in occelmnt are Earth radii per hour
//   - RA in <Star> is hours
//   - Dec in <Star> is degrees
func ShadowVelocityFromOWCEventKmPerSec(
	xmlText string,
	latDeg, lonDeg, hMeters float64,
	dut1Seconds float64,
	xpArcsec, ypArcsec float64,
) (vxKms, vyKms float64, err error) {

	// ---------------- Parse XML ----------------
	var occ Occultations
	if err = xml.Unmarshal([]byte(xmlText), &occ); err != nil {
		return
	}
	if len(occ.Events) == 0 {
		err = fmt.Errorf("no Event found in XML")
		return
	}
	ev := occ.Events[0]

	pe, err := parseElementsCSV(ev.Elements)
	if err != nil {
		return
	}

	raRad, decRad, err := parseStarRADec(ev.Star)
	if err != nil {
		return
	}

	// ---------------- Build event UTC from Elements ----------------
	parts := splitCSVPreserveEmpty(strings.TrimSpace(ev.Elements))
	if len(parts) < 6 {
		err = fmt.Errorf("elements missing date fields")
		return
	}

	yearF, e := mustFloat(parts[2])
	if e != nil {
		err = fmt.Errorf("elements year parse: %w", e)
		return
	}
	monthF, e := mustFloat(parts[3])
	if e != nil {
		err = fmt.Errorf("elements month parse: %w", e)
		return
	}
	dayF, e := mustFloat(parts[4])
	if e != nil {
		err = fmt.Errorf("elements day parse: %w", e)
		return
	}

	// UT (hours) -> h:m:s.ns
	utSeconds := pe.UT * 3600.0
	h := int(utSeconds / 3600.0)
	utSeconds -= float64(h) * 3600.0
	m := int(utSeconds / 60.0)
	utSeconds -= float64(m) * 60.0
	s := int(utSeconds)
	ns := int((utSeconds - float64(s)) * 1e9)

	tUTC := time.Date(int(yearF), time.Month(int(monthF)), int(dayF), h, m, s, ns, time.UTC)

	// ---------------- Shadow velocity from occelmnt (km/s) ----------------
	// dX,dY are Earth radii/hour
	vshadowKms := earthRadiiPerHourToKmPerSec(pe.dX, pe.dY)

	// ---------------- Combine with observer motion ----------------
	vrelKms, _ := relativeShadowVelocityFromEvent(
		tUTC, dut1Seconds, xpArcsec, ypArcsec,
		latDeg, lonDeg, hMeters,
		raRad, decRad,
		vshadowKms,
	)

	return vrelKms.Vx, vrelKms.Vy, nil
}

const (
	// WGS84
	wgs84A  = 6378137.0 // meters
	wgs84F  = 1.0 / 298.257223563
	wgs84E2 = wgs84F * (2 - wgs84F)

	// Earth rotation
	omegaEarth = 7.2921150e-5 // rad/s (nominal)

	// ReKm Earth radius is used by many occultation element sets (km).
	// If your occelmnt elements assume a different Earth radius, change this constant.
	ReKm = 6378.1366
)

// --------------------- vector math ---------------------

type Vec3 struct{ X, Y, Z float64 }

func (a Vec3) Mul(s float64) Vec3 { return Vec3{a.X * s, a.Y * s, a.Z * s} }

func Dot(a, b Vec3) float64 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }

func Cross(a, b Vec3) Vec3 {
	return Vec3{
		a.Y*b.Z - a.Z*b.Y,
		a.Z*b.X - a.X*b.Z,
		a.X*b.Y - a.Y*b.X,
	}
}

func Norm(a Vec3) float64 { return math.Sqrt(Dot(a, a)) }

func Normalize(a Vec3) Vec3 {
	n := Norm(a)
	if n == 0 {
		return Vec3{}
	}
	return a.Mul(1 / n)
}

// Rz Rotate about Z by angle theta (right-handed)
func Rz(theta float64, v Vec3) Vec3 {
	c := math.Cos(theta)
	s := math.Sin(theta)
	return Vec3{
		c*v.X - s*v.Y,
		s*v.X + c*v.Y,
		v.Z,
	}
}

// Small-angle polar motion rotation.
// For most occultation velocity work, xp, yp are tiny; this is adequate.
func applyPolarMotion(rECEF Vec3, xpRad, ypRad float64) Vec3 {
	// Rx(-yp)
	cx := math.Cos(-ypRad)
	sx := math.Sin(-ypRad)
	r1 := Vec3{
		rECEF.X,
		cx*rECEF.Y - sx*rECEF.Z,
		sx*rECEF.Y + cx*rECEF.Z,
	}
	// Ry(-xp)
	cy := math.Cos(-xpRad)
	sy := math.Sin(-xpRad)
	return Vec3{
		cy*r1.X + sy*r1.Z,
		r1.Y,
		-sy*r1.X + cy*r1.Z,
	}
}

func deg2rad(d float64) float64 { return d * math.Pi / 180.0 }

// --------------------- time / ERA ---------------------

func julianDateUTC(t time.Time) float64 {
	u := t.UTC()
	year, month, day := u.Date()
	h := float64(u.Hour())
	m := float64(u.Minute())
	s := float64(u.Second()) + float64(u.Nanosecond())*1e-9

	if month <= 2 {
		year--
		month += 12
	}
	A := int(float64(year) / 100.0)
	B := 2 - A + int(float64(A)/4.0)

	dayFrac := (h + (m+s/60.0)/60.0) / 24.0
	JD := math.Floor(365.25*float64(year+4716)) +
		math.Floor(30.6001*float64(month+1)) +
		float64(day) + float64(B) - 1524.5 + dayFrac
	return JD
}

// Earth Rotation Angle (IAU 2000) needs UT1 JD
func earthRotationAngle(jdUT1 float64) float64 {
	d := jdUT1 - 2451545.0
	theta := 2 * math.Pi * (0.7790572732640 + 1.00273781191135448*d)
	theta = math.Mod(theta, 2*math.Pi)
	if theta < 0 {
		theta += 2 * math.Pi
	}
	return theta
}

// --------------------- geodesy ---------------------

// Geodetic to ECEF (meters)
func geodeticToECEF(latRad, lonRad, hMeters float64) Vec3 {
	sinLat := math.Sin(latRad)
	cosLat := math.Cos(latRad)
	sinLon := math.Sin(lonRad)
	cosLon := math.Cos(lonRad)

	N := wgs84A / math.Sqrt(1.0-wgs84E2*sinLat*sinLat)
	x := (N + hMeters) * cosLat * cosLon
	y := (N + hMeters) * cosLat * sinLon
	z := (N*(1.0-wgs84E2) + hMeters) * sinLat
	return Vec3{x, y, z}
}

// Observer velocity in the inertial-ish frame due to Earth rotation.
// Uses UT1 (via dut1Seconds) for ERA; includes optional polar motion xp/yp (arcsec).
func observerVelocityECI(
	tUTC time.Time,
	dut1Seconds float64,
	latRad, lonRad, hMeters float64,
	xpArcsec, ypArcsec float64,
) Vec3 {

	rECEF := geodeticToECEF(latRad, lonRad, hMeters)

	// polar motion (arcsec -> rad)
	xpRad := xpArcsec * (math.Pi / 180.0) / 3600.0
	ypRad := ypArcsec * (math.Pi / 180.0) / 3600.0
	rPEF := applyPolarMotion(rECEF, xpRad, ypRad)

	jdUTC := julianDateUTC(tUTC)
	jdUT1 := jdUTC + dut1Seconds/86400.0
	era := earthRotationAngle(jdUT1)

	omega := Vec3{0, 0, omegaEarth}
	vPEF := Cross(omega, rPEF) // m/s

	// rotate into inertial-ish frame
	vECI := Rz(era, vPEF)
	return vECI
}

// --------------------- fundamental plane from RA/Dec ---------------------

func starUnitFromRADec(raRad, decRad float64) Vec3 {
	c := math.Cos(decRad)
	return Normalize(Vec3{
		c * math.Cos(raRad),
		c * math.Sin(raRad),
		math.Sin(decRad),
	})
}

// Plane basis vectors (e1,e2) spanning plane perpendicular to sHat.
func fundamentalPlaneBasis(sHat Vec3) (e1, e2 Vec3) {
	k := Vec3{0, 0, 1}
	tmp := Cross(k, sHat)
	if Norm(tmp) < 1e-8 {
		i := Vec3{1, 0, 0}
		tmp = Cross(i, sHat)
	}
	e1 = Normalize(tmp)
	e2 = Normalize(Cross(sHat, e1))
	return e1, e2
}

func projectToPlane(v Vec3, e1, e2 Vec3) (vx, vy float64) {
	return Dot(v, e1), Dot(v, e2)
}

// --------------------- occelmnt parsing ---------------------

type Occultations struct {
	XMLName xml.Name `xml:"Occultations"`
	Events  []Event  `xml:"Event"`
}

type Event struct {
	Elements string `xml:"Elements"`
	Star     string `xml:"Star"`
	Object   string `xml:"Object"`
	ObjectLC string `xml:"object"`
}

type ParsedElements struct {
	Tag string // e.g. "JPL#72:2025-09-17@2026-04-03[OWC]"
	// The fields below assume the common OWC ordering:
	// ... , <UT>, <X>, <Y>, <dX>, <dY>, <d2X>, <d2Y>, ...
	UT   float64
	X    float64
	Y    float64
	dX   float64 // Earth radii per hour (per your file)
	dY   float64 // Earth radii per hour (per your file)
	d2X  float64 // (often ER/hr^2) if present
	d2Y  float64
	rest []float64
}

// Parse the comma-separated <Elements> payload.
func parseElementsCSV(s string) (ParsedElements, error) {
	parts := splitCSVPreserveEmpty(strings.TrimSpace(s))
	if len(parts) < 10 {
		return ParsedElements{}, fmt.Errorf("elements: expected >=10 comma-separated fields, got %d", len(parts))
	}

	pe := ParsedElements{}
	pe.Tag = strings.TrimSpace(parts[0])

	// field 1 is often "shadow speed?" or another scalar; your example has 2.76.
	// fields 2..4 are date (Y, M, D)
	// field 5 is UT (hours) in your example: 8.7318337
	// then X, Y, dX, dY, ...
	// Index map for your example line:
	// 0 tag
	// 1 scalar
	// 2 year
	// 3 month
	// 4 day
	// 5 UT_hours
	// 6 X
	// 7 Y
	// 8 dX
	// 9 dY
	// 10 d2X
	// 11 d2Y
	// 12 ...
	// 13 ...
	var err error
	pe.UT, err = mustFloat(parts[5])
	if err != nil {
		return ParsedElements{}, fmt.Errorf("elements UT parse: %w", err)
	}
	pe.X, err = mustFloat(parts[6])
	if err != nil {
		return ParsedElements{}, fmt.Errorf("elements X parse: %w", err)
	}
	pe.Y, err = mustFloat(parts[7])
	if err != nil {
		return ParsedElements{}, fmt.Errorf("elements Y parse: %w", err)
	}
	pe.dX, err = mustFloat(parts[8])
	if err != nil {
		return ParsedElements{}, fmt.Errorf("elements dX parse: %w", err)
	}
	pe.dY, err = mustFloat(parts[9])
	if err != nil {
		return ParsedElements{}, fmt.Errorf("elements dY parse: %w", err)
	}

	// Optional accelerations if present:
	if len(parts) > 11 {
		pe.d2X, _ = mustFloat(parts[10])
		pe.d2Y, _ = mustFloat(parts[11])
	}
	for i := 12; i < len(parts); i++ {
		if parts[i] == "" {
			continue
		}
		v, e := mustFloat(parts[i])
		if e == nil {
			pe.rest = append(pe.rest, v)
		}
	}
	return pe, nil
}

// Parse <Star> line and return RA, Dec.
// In your example: "UCAC4 ...,16.22554060,4.3842994,..."
// That RA is almost certainly in HOURS (0..24). We'll treat it as hours.
// Dec is in degrees.
func parseStarRADec(starCSV string) (raRad, decRad float64, err error) {
	parts := splitCSVPreserveEmpty(strings.TrimSpace(starCSV))
	if len(parts) < 3 {
		return 0, 0, fmt.Errorf("star: expected at least 3 fields, got %d", len(parts))
	}
	raHours, err := mustFloat(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("star RA parse: %w", err)
	}
	decDeg, err := mustFloat(parts[2])
	if err != nil {
		return 0, 0, fmt.Errorf("star Dec parse: %w", err)
	}
	raDeg := raHours * 15.0
	return deg2rad(raDeg), deg2rad(decDeg), nil
}

// Minimal CSV splitter (OWC style doesn’t quote commas in these fields).
func splitCSVPreserveEmpty(s string) []string {
	// Split on commas; preserve empty fields.
	raw := strings.Split(s, ",")
	for i := range raw {
		raw[i] = strings.TrimSpace(raw[i])
	}
	return raw
}

func mustFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	return strconv.ParseFloat(s, 64)
}

// --------------------- velocity combination ---------------------

type PlaneVel struct {
	Vx float64 // plane-x component
	Vy float64 // plane-y component
}

// Convert Earth radii per hour to km/s.
func earthRadiiPerHourToKmPerSec(dxErph, dyErph float64) PlaneVel {
	scale := ReKm / 3600.0
	return PlaneVel{Vx: dxErph * scale, Vy: dyErph * scale}
}

// FresnelScale returns the Fresnel scale in km for the given observation wavelength (nm) and distance (AU).
func FresnelScale(wavelengthNm, ZAu float64) float64 {
	auToKm := 1.495979e+8 // Convert distance expressed in AU to km
	nmToKm := 1e-9 * 1e-3 // Convert nm to km
	wavelengthKm := wavelengthNm * nmToKm
	ZKm := ZAu * auToKm
	return math.Sqrt(wavelengthKm * ZKm / 2)
}

// Compute relative shadow velocity on the plane: vRel = vShadow - vObs (both on the same plane basis).
func relativeShadowVelocityFromEvent(
	tUTC time.Time,
	dut1Seconds float64,
	xpArcsec, ypArcsec float64,
	latDeg, lonDeg, hMeters float64,
	raRad, decRad float64,
	vshadowplaneKms PlaneVel,
) (vrelKms PlaneVel, vobsplaneKms PlaneVel) {

	latRad := deg2rad(latDeg)
	lonRad := deg2rad(lonDeg)

	vobseciMps := observerVelocityECI(tUTC, dut1Seconds, latRad, lonRad, hMeters, xpArcsec, ypArcsec)

	// Build plane basis from the star direction
	sHat := starUnitFromRADec(raRad, decRad)
	e1, e2 := fundamentalPlaneBasis(sHat)

	// Project observer velocity onto plane and convert m/s -> km/s
	vxObs, vyObs := projectToPlane(vobseciMps.Mul(1e-3), e1, e2)
	vobsplaneKms = PlaneVel{Vx: vxObs, Vy: vyObs}

	// Relative velocity experienced by observer
	vrelKms = PlaneVel{
		Vx: vshadowplaneKms.Vx - vobsplaneKms.Vx,
		Vy: vshadowplaneKms.Vy - vobsplaneKms.Vy,
	}
	return vrelKms, vobsplaneKms
}
