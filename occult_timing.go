// Package occult computes the local time at which an asteroid occultation
// reaches minimum distance from the shadow axis at a given observation site.
//
// Port of OccultWatcher's FundPlane.GetTforMinimumDistance, with the full
// apparent-place reduction (precession + IAU 1980 nutation + annual aberration)
// from AstroUtilities.ApparentStarPosition.
//
// Minimum inputs (per event):
//
//	year, month, day                  (event date)
//	midTimeUT                         (geocentric mid-event time, UT hours)
//	bessX,  bessY                     (shadow position on fundamental plane)
//	bessXp, bessYp                    (first derivatives)
//	bessXs, bessYs                    (second derivatives)
//	subStellarLong                    (sub-stellar longitude, degrees east)
//	starRAHours, starDEDeg            (J2000 catalog star position)
//
// Optional (only needed for sun altitude / day-night check):
//
//	subSolarLong, subSolarLat         (degrees)
//
// Per observer:
//
//	siteLonDeg                        (east-positive)
//	siteLatDeg
//	siteAltM                          (above the geoid)
package main

import (
	"bytes"
	"compress/flate"
	_ "embed"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// -- Constants from the C# source --------------------------------------------

const (
	radian                  = 180.0 / math.Pi
	earthRadiusKm           = 6378.137                  // WGS-84 equatorial radius
	earthFlatteningFactor   = 0.993305620009859         // (1 - f)^2, f = 1/298.257
	earthRotationRadPerHour = 0.2625161                 // ~15.04108 deg/hr, sidereal
	newtonTolHours          = 1e-5
	newtonMaxIter           = 30
)

// -- Inputs ------------------------------------------------------------------

// Occelmnt holds the fields GetTforMinimumDistance actually reads, plus the
// values derived from them at construction time.
type Occelmnt struct {
	// Event date (from <Elements>)
	Year, Month, Day int
	MidTimeUT        float64 // hours

	// Besselian elements (from <Elements>)
	BessX, BessY   float64
	BessXp, BessYp float64 // X', Y'
	BessXs, BessYs float64 // X'', Y''

	// Star position (from <Star>) -- J2000 catalog values
	StarRAHours float64
	StarDEDeg   float64

	// Visual magnitudes used to predict the occultation drop magnitude.
	// StarVMag comes from <Star> 1-indexed entry 5; ObjectVMag from <Object>
	// 1-indexed entry 13. Zero means "not available in this XML".
	StarVMag   float64
	ObjectVMag float64

	// Earth geometry (from <Earth>)
	SubStellarLongRaw float64 // degrees; will be corrected to apparent place
	SubSolarLong      float64 // degrees; only needed for ZSun
	SubSolarLat       float64 // degrees; only needed for ZSun

	// Derived after construction
	FPlaneRADeg    float64
	FPlaneDecDeg   float64
	SubStellarLong float64   // corrected
	EventDateTime  time.Time // UTC, with MidTimeUT folded in
}

// NewOccelmnt builds an Occelmnt from the raw values, applying the same
// derived computations as the C# constructor.
func NewOccelmnt(
	year, month, day int,
	midTimeUT float64,
	bessX, bessY, bessXp, bessYp, bessXs, bessYs float64,
	starRAHours, starDEDeg float64,
	subStellarLongRaw, subSolarLong, subSolarLat float64,
) Occelmnt {
	o := Occelmnt{
		Year: year, Month: month, Day: day,
		MidTimeUT: midTimeUT,
		BessX:     bessX, BessY: bessY,
		BessXp: bessXp, BessYp: bessYp,
		BessXs: bessXs, BessYs: bessYs,
		StarRAHours:       starRAHours,
		StarDEDeg:         starDEDeg,
		SubStellarLongRaw: subStellarLongRaw,
		SubSolarLong:      subSolarLong,
		SubSolarLat:       subSolarLat,
	}

	// 1. Apparent star position at the event epoch. The C# Occelmnt constructor
	// calls ApparentStarPosition with muRA=muDec=0 and equinox=2000, treating
	// the OW catalog values as J2000 mean places without proper motion.
	jd := julianDate(year, month, day, midTimeUT)
	raRad, decRad := ApparentStarPosition(
		starRAHours*15.0/radian,
		starDEDeg/radian,
		0.0, 0.0, 2000, jd,
	)
	o.FPlaneRADeg = raRad * radian
	o.FPlaneDecDeg = decRad * radian

	// 2. Sub-stellar longitude correction (Occelmnt ctor lines 481-489).
	ssl := subStellarLongRaw + o.FPlaneRADeg - starRAHours*15.0
	ssl = math.Mod(ssl, 360.0)
	if ssl < 0 {
		ssl += 360.0
	}
	o.SubStellarLong = ssl

	// 3. EventDateTime with MidTimeUT already added (ctor line 503).
	base := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	nanos := int64(midTimeUT * float64(time.Hour))
	o.EventDateTime = base.Add(time.Duration(nanos))

	return o
}

// -- Apparent star position --------------------------------------------------
//
// Direct port of OccultWatcher.AstroUtilities.ApparentStarPosition together
// with its Precession, Nutation, and Aberration helpers. Uses the IAU 1980
// nutation series (63 terms) and an Earth aberration series (3539 terms)
// embedded as compressed binaries via go:embed.
//
// Coordinate flow:
//   J2000 catalog (RA, Dec) + proper motion
//   -> Precession to event epoch (mean place of date)
//   -> Nutation in longitude and obliquity (true place of date)
//   -> Annual aberration (apparent place of date)

// Embedded binary tables. These mirror the OccultWatcher.Core embedded
// resources Earth.bin and Nutation.bin. Both are OWZ!-framed (4-byte
// "OWZ!" magic followed by raw deflate-compressed body) and are decompressed
// at init time into native record slices.
//
//go:embed Earth.bin
var rawEarthBin []byte

//go:embed Nutation.bin
var rawNutationBin []byte

// EarthAberrationData mirrors the C# struct of the same name. Used by the
// Aberration routine -- each record contributes one term to the Earth
// aberration series.
type EarthAberrationData struct {
	I1, I2 int16
	D1     float64
	D2     float64
	D3     float64
}

// EarthNutationData mirrors the C# struct of the same name. Each record
// contributes one term to the IAU 1980 nutation series.
type EarthNutationData struct {
	I1, I2, I3, I4, I5 int16
	D1                 float64
	F1                 float32
	D2                 float64
	I6                 int16
}

var (
	aberrationArgs []EarthAberrationData
	nutationArgs   []EarthNutationData

	// Cached nutation/aberration values, valid for ~0.3 days around accOldJD.
	// Mirrors the static caching in AstroUtilities (s_AccOldJD etc.).
	accOldJD          float64
	accNutLonArcsec   float64
	accNutOblArcsec   float64
	accEcliptic       float64
	accC, accD        float64
)

func init() {
	var err error
	aberrationArgs, err = loadAberrationRecords(rawEarthBin)
	if err != nil {
		panic(fmt.Sprintf("decode Earth.bin: %v", err))
	}
	nutationArgs, err = loadNutationRecords(rawNutationBin)
	if err != nil {
		panic(fmt.Sprintf("decode Nutation.bin: %v", err))
	}
}

// decompressOWZ strips the 4-byte "OWZ!" magic and inflates the raw deflate
// body. If the magic is absent, returns the input unchanged (some resources
// are stored uncompressed).
func decompressOWZ(data []byte) ([]byte, error) {
	if len(data) < 4 || string(data[:4]) != "OWZ!" {
		return data, nil
	}
	r := flate.NewReader(bytes.NewReader(data[4:]))
	defer r.Close()
	return io.ReadAll(r)
}

func loadAberrationRecords(raw []byte) ([]EarthAberrationData, error) {
	body, err := decompressOWZ(raw)
	if err != nil {
		return nil, err
	}
	const recSize = 28 // 2*int16 + 3*float64
	n := len(body) / recSize
	out := make([]EarthAberrationData, n)
	r := bytes.NewReader(body)
	for i := 0; i < n; i++ {
		if err := binary.Read(r, binary.LittleEndian, &out[i].I1); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.LittleEndian, &out[i].I2); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.LittleEndian, &out[i].D1); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.LittleEndian, &out[i].D2); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.LittleEndian, &out[i].D3); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func loadNutationRecords(raw []byte) ([]EarthNutationData, error) {
	body, err := decompressOWZ(raw)
	if err != nil {
		return nil, err
	}
	const recSize = 32 // 5*int16 + double + float32 + double + int16
	n := len(body) / recSize
	out := make([]EarthNutationData, n)
	r := bytes.NewReader(body)
	for i := 0; i < n; i++ {
		for _, p := range []interface{}{
			&out[i].I1, &out[i].I2, &out[i].I3, &out[i].I4, &out[i].I5,
			&out[i].D1, &out[i].F1, &out[i].D2, &out[i].I6,
		} {
			if err := binary.Read(r, binary.LittleEndian, p); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// Precession is a direct port of AstroUtilities.Precession.
// StartEquinox: 1950 (B1950) or 2000 (J2000). RA and Dec are in radians;
// muRA and muDec are proper motion in radians per year.
func Precession(startEquinox int, jdEnd float64, ra, dec *float64, muRA, muDec float64) {
	if startEquinox == 1950 {
		// B1950 -> J2000 chain; not exercised in our XML pipeline but ported
		// for fidelity to the C# source.
		*ra += 50.0 * muRA
		*dec += 50.0 * muDec
		*ra += 0.0055878714
		num := math.Cos(*dec) * math.Sin(*ra)
		num2 := math.Cos(0.0048580348)*math.Cos(*dec)*math.Cos(*ra) - math.Sin(0.0048580348)*math.Sin(*dec)
		num3 := math.Cos(0.0048580348)*math.Sin(*dec) + math.Sin(0.0048580348)*math.Cos(*dec)*math.Cos(*ra)
		*ra = math.Atan2(num, num2) + 0.0055888302
		*dec = math.Atan(num3 / math.Sqrt(num*num+num2*num2))
		muRA = muRA + 0.0048581*(muRA*math.Cos(*ra)*math.Tan(*dec)+muDec*math.Sin(*ra)/math.Cos(*dec)/math.Cos(*dec)) - 2.114e-08*math.Sin(*ra)*math.Tan(*dec) + 6.7e-09
		muDec = muDec - 0.0048581*muRA*math.Sin(*ra) - 2.114e-08*math.Cos(*ra)
		*ra = *ra + 1.651e-06*math.Sin(*ra+2.945)/math.Cos(*dec) + 5.636e-06
		*dec = *dec + 1.653e-06*math.Cos(*ra+2.945)*math.Sin(*dec) + 1.406e-07*math.Cos(*dec)
		if *ra < 0.0 {
			*ra += math.Pi * 2.0
		}
		if *ra > math.Pi*2.0 {
			*ra -= math.Pi * 2.0
		}
	}

	t := (jdEnd - 2451545.0) / 36525.0
	zeta := (0.640616139*t + 8.3856e-05*t*t + 4.9994e-06*t*t*t) / radian
	zRot := (0.640616139*t + 0.000304078*t*t + 5.0564e-06*t*t*t) / radian
	theta := (0.556753028*t - 0.000118514*t*t - 1.16203e-05*t*t*t) / radian

	*ra += muRA * t * 100.0
	*dec += muDec * t * 100.0
	*ra += zeta

	num := math.Cos(*dec) * math.Sin(*ra)
	num2 := math.Cos(theta)*math.Cos(*dec)*math.Cos(*ra) - math.Sin(theta)*math.Sin(*dec)
	num3 := math.Cos(theta)*math.Sin(*dec) + math.Sin(theta)*math.Cos(*dec)*math.Cos(*ra)

	*ra = math.Atan2(num, num2) + zRot
	if *ra < 0.0 {
		*ra += math.Pi * 2.0
	}
	if *ra > math.Pi*2.0 {
		*ra -= math.Pi * 2.0
	}
	*dec = math.Atan(num3 / math.Sqrt(num*num+num2*num2))
}

// Nutation is a direct port of AstroUtilities.Nutation. Returns the
// nutation in longitude and obliquity in arcseconds, plus the true
// obliquity of the ecliptic in radians.
func Nutation(jd float64) (longitudeArcsec, obliquityArcsec, ecliptic float64) {
	t := (jd - 2451545.0) / 36525.0

	// Fundamental arguments (radians)
	d := (297.8502042 + 445267.1115168*t - 0.0016335*t*t + t*t*t/546300.0) / radian
	f := (93.2720993 + 483202.0175273*t - 0.0034064*t*t - t*t*t/6550000.0) / radian
	mSun := (357.5291092 + 35999.0502909*t - 0.000156*t*t - t*t*t/2280000.0) / radian
	mMoon := (134.9634114 + 477198.8676313*t + 0.0089937*t*t + t*t*t/73725.0) / radian
	omega := (218.3164591+481267.88134236*t-0.0013298*t*t+t*t*t/546300.0)/radian - f

	var sumLon, sumObl float64
	for _, e := range nutationArgs {
		arg := float64(e.I1)*d + float64(e.I2)*mSun + float64(e.I3)*mMoon + float64(e.I4)*f + float64(e.I5)*omega
		sumLon += (e.D1 + float64(e.F1)*t) * math.Sin(arg)
		sumObl += (e.D2 + float64(e.I6)*t) * math.Cos(arg)
	}

	longitudeArcsec = sumLon / 10000.0
	obliquityArcsec = sumObl / 10000.0
	ecliptic = (23.4392911 - 0.01300417*t - 1.64e-06*t*t + 5.036e-07*t*t*t + obliquityArcsec/3600.0) / radian
	return
}

// Aberration is a direct port of AstroUtilities.Aberration. Returns the
// annual aberration C and D coefficients in radians.
func Aberration(jd float64) (c, d float64) {
	const (
		obliquityCoeff0 = 23.4392911
		obliquityCoeff1 = 0.1300417
	)
	tMillennia := (jd - 2451545.0) / 365250.0

	// Index 0 is unused; the C# code uses 1-based indexing matching its
	// EarthAberrationData.I2 multiplier indices.
	tPow := [7]float64{
		0.0,
		1.0,
		tMillennia,
		tMillennia * tMillennia,
		math.Pow(tMillennia, 4.0),
		math.Pow(tMillennia, 8.0),
		math.Pow(tMillennia, 16.0),
	}

	precRate := (13.96971*tPow[2] + 0.03086*tPow[3]) / radian
	cosObl := math.Cos((obliquityCoeff0 - obliquityCoeff1*tPow[2]) / radian)

	var xdot, ydot float64
	for _, e := range aberrationArgs {
		angle := e.D2 + e.D3*tPow[2]
		// I1: which axis this term contributes to (1 = X, 2 = Y, 3 = unused).
		// I2: index into tPow series.
		if e.I1 != 3 {
			contribution := -e.D1 * (float64(e.I2)*math.Cos(angle)*tPow[e.I2] - e.D3*math.Sin(angle)*tPow[e.I2+1]) / 365250.0
			switch e.I1 {
			case 1:
				xdot += contribution
			case 2:
				ydot += contribution
			}
		}
		if e.D1 < 1e-05 {
			break
		}
	}

	xdot5 := 1191.28 * xdot / radian / 3600.0
	ydot6 := -1191.28 * ydot * cosObl / radian / 3600.0
	c = ydot6 - precRate*cosObl*xdot5
	d = xdot5 + precRate/cosObl*ydot6
	return
}

// ApparentStarPosition is a direct port of AstroUtilities.ApparentStarPosition.
// Applies precession (with proper motion), nutation, and annual aberration
// to convert a catalog J2000 (or B1950) star position to the apparent place
// at jd.
//
// raRad, decRad are catalog coordinates in radians.
// muRA, muDec are proper motions in radians per year.
// equinox is 1950 or 2000.
// Returns (apparentRA, apparentDec) in radians.
func ApparentStarPosition(raRad, decRad, muRA, muDec float64, equinox int, jd float64) (float64, float64) {
	ra, dec := raRad, decRad
	Precession(equinox, jd, &ra, &dec, muRA, muDec)

	// Cache nutation and aberration values; they change slowly so reuse them
	// for any subsequent calls within ~0.3 days of the same JD. This matches
	// the s_AccOldJD logic in the C# source exactly.
	var lonArcsec, oblArcsec, ecl, cCoef, dCoef float64
	if math.Abs(jd-accOldJD) > 0.3 {
		lonArcsec, oblArcsec, ecl = Nutation(jd)
		cCoef, dCoef = Aberration(jd)
		accOldJD = jd
		accNutLonArcsec = lonArcsec
		accNutOblArcsec = oblArcsec
		accEcliptic = ecl
		accC = cCoef
		accD = dCoef
	} else {
		jd = accOldJD
		lonArcsec = accNutLonArcsec
		oblArcsec = accNutOblArcsec
		ecl = accEcliptic
		cCoef = accC
		dCoef = accD
	}

	// Apply nutation to RA and Dec
	ra += (lonArcsec*(math.Cos(ecl)+math.Sin(ecl)*math.Sin(ra)*math.Tan(dec)) -
		oblArcsec*math.Cos(ra)*math.Tan(dec)) / 3600.0 / radian
	dec += (lonArcsec*math.Sin(ecl)*math.Cos(ra) + oblArcsec*math.Sin(ra)) / 3600.0 / radian

	// Apply annual aberration
	ra += (cCoef*math.Cos(ra) + dCoef*math.Sin(ra)) / math.Cos(dec)
	dec += (cCoef*(math.Tan(ecl)/math.Tan(dec)-math.Sin(ra)) + dCoef*math.Cos(ra)) * math.Sin(dec)

	return ra, dec
}

// julianDate returns JD for a UT calendar date (Gregorian).
func julianDate(year, month, day int, hourUT float64) float64 {
	if month <= 2 {
		year--
		month += 12
	}
	a := year / 100
	b := 2 - a + a/4
	jd := math.Floor(365.25*float64(year+4716)) +
		math.Floor(30.6001*float64(month+1)) +
		float64(day) + float64(b) - 1524.5
	return jd + hourUT/24.0
}

// -- Core routine: direct port of GetTforMinimumDistance ---------------------

// TimingResult is the output of GetTforMinimumDistance.
type TimingResult struct {
	Ta            float64   // hours from EventDateTime
	H1            float64   // local hour angle at minimum, radians
	MinDistanceKm float64   // perpendicular distance, +ve = outside path
	X, Y          float64   // shadow position at solution
	Z             float64   // star direction cosine (sin of altitude)
	ZSun          float64   // sun direction cosine
	EventTimeUTC  time.Time // absolute UTC of closest approach at site
	Iterations    int
	Converged     bool
}

// GetTforMinimumDistance solves for the time offset Ta (hours) at which the
// observer site is at minimum perpendicular distance from the asteroid's
// shadow axis. Direct port of FundPlane.GetTforMinimumDistance.
func GetTforMinimumDistance(
	occ Occelmnt,
	longObsDeg, latObsDeg, geoidAltM float64,
	shadowDistance, taInitial float64,
) TimingResult {
	latRad := latObsDeg / radian
	cosLat := math.Cos(latRad)
	sinLat := math.Sin(latRad)

	// Geocentric observer coordinates (rho cos phi', rho sin phi') with altitude.
	num := 1.0 / math.Sqrt(cosLat*cosLat+earthFlatteningFactor*sinLat*sinLat)
	num2 := earthFlatteningFactor * num
	rhoCosPhi := cosLat * (num + geoidAltM/(earthRadiusKm*1000.0))
	rhoSinPhi := sinLat * (num2 + geoidAltM/(earthRadiusKm*1000.0))

	fpDec := occ.FPlaneDecDeg / radian
	cosFpd := math.Cos(fpDec)
	sinFpd := math.Sin(fpDec)

	Ta := taInitial
	iterations := 0
	correction := 0.0

	var u, v, up, vp, vmag, H1, X, Y float64

	for {
		// Local hour angle of the star at the observer site.
		H1 = -occ.SubStellarLong/radian + earthRotationRadPerHour*Ta + longObsDeg/radian

		// Shadow axis position on fundamental plane at time Ta.
		X = occ.BessX + occ.BessXp*Ta + occ.BessXs*Ta*Ta
		Y = occ.BessY + occ.BessYp*Ta + occ.BessYs*Ta*Ta

		// Observer position projected onto fundamental plane, then offset.
		u = X - rhoCosPhi*math.Sin(H1)
		v = Y - rhoSinPhi*cosFpd + rhoCosPhi*math.Cos(H1)*sinFpd

		// Relative velocity of shadow w.r.t. observer.
		up = occ.BessXp + 2.0*occ.BessXs*Ta - earthRotationRadPerHour*rhoCosPhi*math.Cos(H1)
		vp = occ.BessYp + 2.0*occ.BessYs*Ta - earthRotationRadPerHour*rhoCosPhi*math.Sin(H1)*sinFpd

		vmagSq := up*up + vp*vp
		vmag = math.Sqrt(vmagSq)

		// Newton step: (r . v) / |v|^2.
		correction = (u*up + v*vp) / vmagSq

		// Clamp absurd corrections (line 28-31 in the C# source).
		if math.Abs(correction) > 1.0 {
			correction = math.Copysign(1.0, correction)
		}

		Ta -= correction
		iterations++

		if math.Abs(correction) <= newtonTolHours || iterations >= newtonMaxIter {
			break
		}
	}

	// Minimum perpendicular distance: cross product (r x v) / |v|, signed.
	minDist := (u*vp-v*up)/vmag - shadowDistance
	minDist = -minDist * earthRadiusKm

	// Star altitude direction cosine (sin of altitude above horizon).
	Z := rhoSinPhi*sinFpd + rhoCosPhi*math.Cos(H1)*cosFpd

	// Sun altitude direction cosine.
	sssLat := occ.SubSolarLat / radian
	ZSun := rhoSinPhi*math.Sin(sssLat) +
		rhoCosPhi*math.Cos(H1+(occ.SubStellarLong-occ.SubSolarLong)/radian)*math.Cos(sssLat)

	taNanos := int64(Ta * float64(time.Hour))
	eventTime := occ.EventDateTime.Add(time.Duration(taNanos))

	return TimingResult{
		Ta:            Ta,
		H1:            H1,
		MinDistanceKm: minDist,
		X:             X, Y: Y, Z: Z, ZSun: ZSun,
		EventTimeUTC: eventTime,
		Iterations:   iterations,
		Converged:    math.Abs(correction) <= newtonTolHours,
	}
}

// -- Geoid height lookup -----------------------------------------------------

// Geoid undulation N at observer location, looked up from a 1-degree global
// grid embedded into the binary. Required to convert site altitude above mean
// sea level (orthometric height H) into ellipsoidal height h for the parallax
// calculation in GetTforMinimumDistance:
//
//	h = H + N
//
// Direct port of OccultWatcher.AstroUtilities.GeoidHeight and
// GetSingleGeoidHeightPoint.

//go:embed GeoidHeights.bin
var geoidHeightsData []byte

// GeoidHeight returns the geoid undulation N (meters) at the given longitude
// and latitude (degrees), interpolated bilinearly from the 181x360 grid.
func GeoidHeight(longitudeDeg, latitudeDeg float64) float64 {
	// Normalize longitude to [-180, 180]
	for longitudeDeg < -180.0 {
		longitudeDeg += 360.0
	}
	for longitudeDeg > 180.0 {
		longitudeDeg -= 360.0
	}
	// Edge-case clamp matching the C# source
	if longitudeDeg < -180.0 {
		longitudeDeg += 1.0
	}
	if longitudeDeg > 180.0 {
		longitudeDeg -= 1.0
	}

	lonFloor := int(math.Floor(longitudeDeg))
	latFloor := int(math.Floor(latitudeDeg))

	p00 := geoidPoint(lonFloor, latFloor)
	p10 := geoidPoint(lonFloor+1, latFloor)
	p01 := geoidPoint(lonFloor, latFloor+1)
	p11 := geoidPoint(lonFloor+1, latFloor+1)

	fxHi := float64(lonFloor+1) - longitudeDeg
	rowLow := p00*fxHi + p10*(longitudeDeg-float64(lonFloor))
	rowHigh := p01*(float64(lonFloor+1)-longitudeDeg) + p11*(longitudeDeg-float64(lonFloor))

	fyHi := float64(latFloor+1) - latitudeDeg
	return rowLow*fyHi + rowHigh*(latitudeDeg-float64(latFloor))
}

// geoidPoint returns the raw (signed) geoid height at integer (lon, lat).
// Grid layout: index = (90 - lat) * 360 + 180 + lon, each byte stores meters
// as a signed int8.
func geoidPoint(longitudeDeg, latitudeDeg int) float64 {
	if longitudeDeg > 179 {
		longitudeDeg = -180
	}
	if longitudeDeg < -180 {
		longitudeDeg = -180
	}
	if latitudeDeg > 90 {
		latitudeDeg = 90
	}
	if latitudeDeg < -90 {
		latitudeDeg = -90
	}
	idx := (90-latitudeDeg)*360 + 180 + longitudeDeg
	b := float64(geoidHeightsData[idx])
	if b > 127.0 {
		b -= 256.0
	}
	return b
}

// -- XML parsing -------------------------------------------------------------

// XML schema corresponding to OccultWatcher's Occelmnt.xml format. Each
// element's text content is a comma-separated value list -- the encoding/xml
// package handles the outer structure, and we split CSV in code.

type xmlOccultations struct {
	XMLName xml.Name   `xml:"Occultations"`
	Events  []xmlEvent `xml:"Event"`
}

type xmlEvent struct {
	Elements string `xml:"Elements"`
	Earth    string `xml:"Earth"`
	Star     string `xml:"Star"`
	Object   string `xml:"Object"`
	Orbit    string `xml:"Orbit"`
	Errors   string `xml:"Errors"`
	ID       string `xml:"ID"`
}

// ParseOccelmntXML reads an Occelmnt.xml file and returns one Occelmnt per
// <Event>. Only the fields needed by GetTforMinimumDistance are populated;
// orbital elements, error ellipse, asteroid magnitudes, etc. are skipped.
func ParseOccelmntXML(path string) ([]Occelmnt, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var root xmlOccultations
	if err := xml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse XML: %w", err)
	}

	out := make([]Occelmnt, 0, len(root.Events))
	for i, ev := range root.Events {
		occ, err := eventToOccelmnt(ev)
		if err != nil {
			return nil, fmt.Errorf("event %d: %w", i, err)
		}
		out = append(out, occ)
	}
	return out, nil
}

// eventToOccelmnt parses one <Event> into a populated Occelmnt.
func eventToOccelmnt(ev xmlEvent) (Occelmnt, error) {
	// <Elements> layout (after CSV-splitting):
	//   [0] ephemeris tag: "JPL#58:2025-11-27@2026-03-11[OWC]"
	//   [1] version-ish:   "0.29"
	//   [2..4] year, month, day
	//   [5] MidTimeUT
	//   [6] BessX
	//   [7] BessY
	//   [8] BessXp
	//   [9] BessYp
	//   [10] BessXs
	//   [11] BessYs
	elems := splitCSV(ev.Elements)
	if len(elems) < 12 {
		return Occelmnt{}, fmt.Errorf("Elements has %d fields, need at least 12", len(elems))
	}

	year, err := parseInt(elems[2], "Elements.year")
	if err != nil {
		return Occelmnt{}, err
	}
	month, err := parseInt(elems[3], "Elements.month")
	if err != nil {
		return Occelmnt{}, err
	}
	day, err := parseInt(elems[4], "Elements.day")
	if err != nil {
		return Occelmnt{}, err
	}

	midTimeUT, err := parseFloats1(elems[5], "Elements.MidTimeUT")
	if err != nil {
		return Occelmnt{}, err
	}
	bessX, err := parseFloats1(elems[6], "Elements.BessX")
	if err != nil {
		return Occelmnt{}, err
	}
	bessY, err := parseFloats1(elems[7], "Elements.BessY")
	if err != nil {
		return Occelmnt{}, err
	}
	bessXp, err := parseFloats1(elems[8], "Elements.BessXp")
	if err != nil {
		return Occelmnt{}, err
	}
	bessYp, err := parseFloats1(elems[9], "Elements.BessYp")
	if err != nil {
		return Occelmnt{}, err
	}
	bessXs, err := parseFloats1(elems[10], "Elements.BessXs")
	if err != nil {
		return Occelmnt{}, err
	}
	bessYs, err := parseFloats1(elems[11], "Elements.BessYs")
	if err != nil {
		return Occelmnt{}, err
	}

	// <Earth> layout:
	//   [0] SubStellarLong
	//   [1] SubStellarLat
	//   [2] SubSolarLong
	//   [3] SubSolarLat
	//   [4] flag (bool, unused here)
	earth := splitCSV(ev.Earth)
	if len(earth) < 4 {
		return Occelmnt{}, fmt.Errorf("Earth has %d fields, need at least 4", len(earth))
	}
	subStellarLong, err := parseFloats1(earth[0], "Earth.SubStellarLong")
	if err != nil {
		return Occelmnt{}, err
	}
	subSolarLong, err := parseFloats1(earth[2], "Earth.SubSolarLong")
	if err != nil {
		return Occelmnt{}, err
	}
	subSolarLat, err := parseFloats1(earth[3], "Earth.SubSolarLat")
	if err != nil {
		return Occelmnt{}, err
	}

	// <Star> layout (numeric fields used):
	//   [0] designation:   "J061552.44+032622.3"
	//   [1] StarRAHours
	//   [2] StarDEDeg
	//   [4] StarVMag (1-indexed entry 5)
	star := splitCSV(ev.Star)
	if len(star) < 3 {
		return Occelmnt{}, fmt.Errorf("Star has %d fields, need at least 3", len(star))
	}
	starRAHours, err := parseFloats1(star[1], "Star.StarRAHours")
	if err != nil {
		return Occelmnt{}, err
	}
	starDEDeg, err := parseFloats1(star[2], "Star.StarDEDeg")
	if err != nil {
		return Occelmnt{}, err
	}

	occ := NewOccelmnt(
		year, month, day,
		midTimeUT,
		bessX, bessY, bessXp, bessYp, bessXs, bessYs,
		starRAHours, starDEDeg,
		subStellarLong, subSolarLong, subSolarLat,
	)

	// Optional visual magnitudes (permissive: leave at 0 if absent or unparseable).
	// <Star> 1-indexed entry 5 -> 0-indexed [4]
	// <Object> 1-indexed entry 13 -> 0-indexed [12]
	if len(star) >= 5 {
		if v, perr := strconv.ParseFloat(star[4], 64); perr == nil {
			occ.StarVMag = v
		}
	}
	obj := splitCSV(ev.Object)
	if len(obj) >= 13 {
		if v, perr := strconv.ParseFloat(obj[12], 64); perr == nil {
			occ.ObjectVMag = v
		}
	}

	return occ, nil
}

// splitCSV splits "a, b ,c " into ["a", "b", "c"], trimming whitespace.
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func parseInt(s, field string) (int, error) {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not an integer: %w", field, s, err)
	}
	return v, nil
}

func parseFloats1(s, field string) (float64, error) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a number: %w", field, s, err)
	}
	return v, nil
}

// -- Derived values cached when an occelmnt XML is loaded --------------------

// lastComputedEventUTC holds the most recently computed observer-corrected
// event UTC string, formatted "02 Jan 2006 15:04:05" (matching the details.csv
// "Event Time (UT)" convention). Empty until processOccelmntXML succeeds.
var lastComputedEventUTC string

// lastComputedMagDrop holds the most recently computed magnitude drop (mag)
// derived from the star's V magnitude and the asteroid's V magnitude in the
// loaded occelmnt XML. Zero means "not yet computed for this session".
var lastComputedMagDrop float64

// lastMagDropDetail holds the human-readable summary line produced when
// processOccelmntXML computes a magnitude drop, e.g.
//   "Mag drop from occelmnt.xml: star V=8.460, asteroid V=8.850, combined V=7.885, dMag=0.965 mag, percent drop ~58.88%"
// Used by the Final Report so the same detail is reproduced there.
var lastMagDropDetail string

// ComputeMagDrop returns the (positive) magnitude drop observed when a star
// of visual magnitude starMag is occulted by an asteroid of visual magnitude
// asteroidMag. Combines the two fluxes, then returns asteroidMag minus the
// combined magnitude. Both inputs must be > 0; otherwise the result is 0.
func ComputeMagDrop(starMag, asteroidMag float64) float64 {
	if starMag <= 0 || asteroidMag <= 0 {
		return 0
	}
	fs := math.Pow(10, -starMag/2.5)
	fa := math.Pow(10, -asteroidMag/2.5)
	mCombined := -2.5 * math.Log10(fs+fa)
	return asteroidMag - mCombined
}

// processOccelmntXML parses the given occelmnt XML and caches two derived
// values for the rest of the session:
//   - lastComputedMagDrop: predicted magnitude drop from <Star> V and
//     <Object> V (does not need observer location).
//   - lastComputedEventUTC: observer-corrected event UTC from
//     GetTforMinimumDistance (requires the observer location to be set).
//
// Both values are printed to stdout when they are produced. Returns the
// formatted Event UTC string for callers that want it directly; returns ""
// when the Event UTC could not be computed (XML empty/unparseable, observer
// location not set, or Newton solver did not converge). The mag-drop side
// effect happens whenever the XML parses, independent of the return value.
func processOccelmntXML(xmlStr string) string {
	if xmlStr == "" {
		return ""
	}

	tmp, err := os.CreateTemp("", "occelmnt-*.xml")
	if err != nil {
		return ""
	}
	defer os.Remove(tmp.Name())
	if _, werr := tmp.Write([]byte(xmlStr)); werr != nil {
		tmp.Close()
		return ""
	}
	tmp.Close()

	events, err := ParseOccelmntXML(tmp.Name())
	if err != nil || len(events) == 0 {
		return ""
	}
	occ := events[0]

	// Magnitude drop -- no observer-location dependency.
	if md := ComputeMagDrop(occ.StarVMag, occ.ObjectVMag); md > 0 {
		lastComputedMagDrop = md
		percent := (1 - math.Pow(10, -md/2.5)) * 100
		mCombined := occ.ObjectVMag - md
		fmt.Printf("Computed mag drop: %.3f mag (~%.2f%% flux drop)\n", md, percent)
		lastMagDropDetail = fmt.Sprintf(
			"Mag drop from occelmnt.xml: star V=%.3f, asteroid V=%.3f, combined V=%.3f, dMag=%.3f mag, percent drop ~%.2f%%",
			occ.StarVMag, occ.ObjectVMag, mCombined, md, percent,
		)
		logAction(lastMagDropDetail)
	}

	// Event UTC requires the observer location.
	if !lastObserverLocationSet {
		return ""
	}

	N := GeoidHeight(lastObserverLonDeg, lastObserverLatDeg)
	ellipsoidalAltM := lastObserverAltMeters + N
	r := GetTforMinimumDistance(occ, lastObserverLonDeg, lastObserverLatDeg, ellipsoidalAltM, 0.0, 0.0)
	if !r.Converged {
		return ""
	}

	eventUTC := r.EventTimeUTC.Format("02 Jan 2006 15:04:05")
	lastComputedEventUTC = eventUTC
	fmt.Printf("Computed Event UTC (observer-corrected): %s\n", eventUTC)
	return eventUTC
}
