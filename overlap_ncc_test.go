package main

import (
	"math"
	"math/rand"
	"testing"
)

// TestOverlapNCCIsWindowIndependent verifies the property overlapNCC exists for:
// widening the trim range with baseline-only points deflates the padded NCC
// (every added point carries noise variance the flat 1.0 model cannot explain)
// while the overlap-only NCC stays put, since it sees the same points either way.
func TestOverlapNCCIsWindowIndependent(t *testing.T) {
	// V-shaped dip, 4 seconds long, matching the shape used in overlay_test.go.
	curve := []timeIntensityPoint{
		{time: 0.0, intensity: 1.0},
		{time: 1.0, intensity: 1.0},
		{time: 1.5, intensity: 0.2},
		{time: 2.5, intensity: 0.2},
		{time: 3.0, intensity: 1.0},
		{time: 4.0, intensity: 1.0},
	}
	curveTimes := make([]float64, len(curve))
	for i, pt := range curve {
		curveTimes[i] = pt.time
	}
	pc := &precomputedCurve{
		curve:      curve,
		curveTimes: curveTimes,
		edgeTimes:  []float64{1.0, 3.0},
		duration:   curve[len(curve)-1].time,
	}

	// Wide target: 0–40 sec at 10 fps — the curve itself, placed at a known shift
	// and buried in white noise, so noise is the only thing the model cannot
	// explain. The narrow target is a slice of the same arrays, so points shared
	// by the two windows carry identical noise and the comparison isolates
	// window width.
	rng := rand.New(rand.NewSource(42))
	const sigma = 0.1
	const trueShift = 10.0
	var wideTimes, wideValues []float64
	narrowStart, narrowEnd := -1, -1
	for step := 0; step <= 400; step++ {
		ts := float64(step) * 0.1
		v := 1.0
		if localT := ts - trueShift; localT >= 0 && localT <= pc.duration {
			v = interpolateAt(curve, curveTimes, localT)
		}
		wideTimes = append(wideTimes, ts)
		wideValues = append(wideValues, v+rng.NormFloat64()*sigma)
		if ts >= 10.0 && ts <= 15.0 {
			if narrowStart < 0 {
				narrowStart = step
			}
			narrowEnd = step
		}
	}
	narrowTimes := wideTimes[narrowStart : narrowEnd+1]
	narrowValues := wideValues[narrowStart : narrowEnd+1]

	narrowFR, err := nccSlidingFit(pc, narrowTimes, narrowValues)
	if err != nil {
		t.Fatalf("narrow nccSlidingFit failed: %v", err)
	}
	wideFR, err := nccSlidingFit(pc, wideTimes, wideValues)
	if err != nil {
		t.Fatalf("wide nccSlidingFit failed: %v", err)
	}

	// Both windows must find the same alignment — dilution is a common factor
	// across shifts, so it does not move the argmax.
	if math.Abs(narrowFR.bestShift-wideFR.bestShift) > 0.15 {
		t.Fatalf("bestShift differs between windows: narrow=%.4f wide=%.4f",
			narrowFR.bestShift, wideFR.bestShift)
	}

	t.Logf("narrow (%d pts): NCC=%.4f overlapNCC=%.4f", len(narrowTimes), narrowFR.bestNCC, narrowFR.bestOverlapNCC)
	t.Logf("wide   (%d pts): NCC=%.4f overlapNCC=%.4f", len(wideTimes), wideFR.bestNCC, wideFR.bestOverlapNCC)

	// The padded NCC must visibly deflate on the wider window.
	if wideFR.bestNCC >= narrowFR.bestNCC-0.05 {
		t.Errorf("expected padded NCC to deflate on the wide window: narrow=%.4f wide=%.4f",
			narrowFR.bestNCC, wideFR.bestNCC)
	}

	// The overlap NCC must not.
	if math.Abs(wideFR.bestOverlapNCC-narrowFR.bestOverlapNCC) > 0.02 {
		t.Errorf("overlap NCC is window-dependent: narrow=%.4f wide=%.4f",
			narrowFR.bestOverlapNCC, wideFR.bestOverlapNCC)
	}
	if narrowFR.bestOverlapNCC <= 0.5 {
		t.Errorf("overlap NCC unexpectedly low for a good fit: %.4f", narrowFR.bestOverlapNCC)
	}

	// Shifts that barely reach the data are reported as 0 rather than as a
	// correlation over one or two points.
	if got := wideFR.nccCurve[0]; got.overlapCount < minOverlapForNCC && got.overlapNCC != 0 {
		t.Errorf("overlapNCC=%.4f reported for only %d overlap points", got.overlapNCC, got.overlapCount)
	}
}

// makeChordCurve builds a 4-second trapezoidal dip of the given depth and
// flat-bottom half-width, standing in for the light curve of one candidate chord.
func makeChordCurve(offset, depth, halfWidth float64) *precomputedCurve {
	bottom := 1.0 - depth
	curve := []timeIntensityPoint{
		{time: 0.0, intensity: 1.0},
		{time: 2.0 - halfWidth - 0.5, intensity: 1.0},
		{time: 2.0 - halfWidth, intensity: bottom},
		{time: 2.0 + halfWidth, intensity: bottom},
		{time: 2.0 + halfWidth + 0.5, intensity: 1.0},
		{time: 4.0, intensity: 1.0},
	}
	curveTimes := make([]float64, len(curve))
	for i, pt := range curve {
		curveTimes[i] = pt.time
	}
	return &precomputedCurve{
		pathOffset: offset,
		curve:      curve,
		curveTimes: curveTimes,
		duration:   curve[len(curve)-1].time,
	}
}

// TestOverlapNCCSeparatesPathOffsetCandidates covers the reason path-offset
// selection uses the overlap score: on a wide trim range the padded scores of
// competing chords are all dragged toward the same small number, so the gap the
// argmax has to resolve shrinks toward the noise floor. The overlap scores keep
// their separation.
func TestOverlapNCCSeparatesPathOffsetCandidates(t *testing.T) {
	candidates := []*precomputedCurve{
		makeChordCurve(-5.0, 0.4, 0.3),
		makeChordCurve(0.0, 0.8, 0.5), // the truth
		makeChordCurve(5.0, 0.6, 0.8),
	}
	const truthIdx = 1

	// Target: the true chord at a known shift, on a window that is mostly baseline.
	rng := rand.New(rand.NewSource(7))
	const sigma = 0.05
	const trueShift = 10.0
	truth := candidates[truthIdx]
	var times, values []float64
	for step := 0; step <= 400; step++ {
		ts := float64(step) * 0.1
		v := 1.0
		if localT := ts - trueShift; localT >= 0 && localT <= truth.duration {
			v = interpolateAt(truth.curve, truth.curveTimes, localT)
		}
		times = append(times, ts)
		values = append(values, v+rng.NormFloat64()*sigma)
	}

	padded := make([]float64, len(candidates))
	overlap := make([]float64, len(candidates))
	for i, pc := range candidates {
		fr, err := nccSlidingFit(pc, times, values)
		if err != nil {
			t.Fatalf("candidate %d: nccSlidingFit failed: %v", i, err)
		}
		padded[i] = fr.bestNCC
		overlap[i] = fr.bestOverlapNCC
		t.Logf("candidate %d (offset %+.1f km): padded NCC=%.4f overlap NCC=%.4f",
			i, pc.pathOffset, padded[i], overlap[i])
	}

	if argmax(overlap) != truthIdx {
		t.Errorf("overlap NCC picked candidate %d, want %d", argmax(overlap), truthIdx)
	}

	paddedGap := padded[truthIdx] - secondBest(padded, truthIdx)
	overlapGap := overlap[truthIdx] - secondBest(overlap, truthIdx)
	t.Logf("winning margin: padded=%.4f overlap=%.4f", paddedGap, overlapGap)
	if overlapGap <= paddedGap {
		t.Errorf("expected the overlap score to separate candidates more than the padded score: padded gap=%.4f overlap gap=%.4f",
			paddedGap, overlapGap)
	}
}

// TestMonteCarloRefitSelectsByOverlapNCC checks that a Monte Carlo trial recovers
// the true chord using the same rule runFitSearch applies.
func TestMonteCarloRefitSelectsByOverlapNCC(t *testing.T) {
	candidates := []*precomputedCurve{
		makeChordCurve(-5.0, 0.4, 0.3),
		makeChordCurve(0.0, 0.8, 0.5), // the truth
		makeChordCurve(5.0, 0.6, 0.8),
	}
	truth := candidates[1]

	const trueShift = 10.0
	var times, vals []float64
	for step := 0; step <= 400; step++ {
		ts := float64(step) * 0.1
		v := 1.0
		if localT := ts - trueShift; localT >= 0 && localT <= truth.duration {
			v = interpolateAt(truth.curve, truth.curveTimes, localT)
		}
		times = append(times, ts)
		vals = append(vals, v)
	}
	fr := &fitResult{
		curve:        truth.curve,
		bestShift:    trueShift,
		sampledTimes: times,
		sampledVals:  vals,
	}

	for trial := 0; trial < 20; trial++ {
		mc, err := runMonteCarloRefit(candidates, fr, 0.02, nil, 0)
		if err != nil {
			t.Fatalf("trial %d: runMonteCarloRefit failed: %v", trial, err)
		}
		if mc.pathOffset != truth.pathOffset {
			t.Errorf("trial %d: selected path offset %.1f km, want %.1f km",
				trial, mc.pathOffset, truth.pathOffset)
		}
	}
}

func argmax(v []float64) int {
	best := 0
	for i, x := range v {
		if x > v[best] {
			best = i
		}
	}
	return best
}

// secondBest returns the largest value in v excluding index skip.
func secondBest(v []float64, skip int) float64 {
	best := math.Inf(-1)
	for i, x := range v {
		if i != skip && x > best {
			best = x
		}
	}
	return best
}
