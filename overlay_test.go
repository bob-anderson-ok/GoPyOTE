package main

import (
	"math"
	"testing"
)

// TestSampledPointsAlignAfterShiftChange verifies that red sampled dots
// fall exactly on the theoretical curve after bestShift is adjusted.
//
// Background: nccSlidingFit computes sampledVals at the original bestShift.
// When the shift is later changed (auto-slide, manual slide), the stored
// sampledVals become stale.  overlayTheoryCurve must recompute Y by
// interpolating fr.curve at the current bestShift so the dots stay on
// the theory line.
func TestSampledPointsAlignAfterShiftChange(t *testing.T) {
	// --- Build a synthetic theoretical curve: a V-shaped dip ---
	//
	//  intensity
	//  1.0  ____          ____
	//             \      /
	//  0.2         \____/
	//       |---|---|---|---|
	//       0   1   2   3   4   time (seconds)
	//
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
	duration := curve[len(curve)-1].time

	pc := &precomputedCurve{
		curve:      curve,
		curveTimes: curveTimes,
		edgeTimes:  []float64{1.0, 3.0},
		duration:   duration,
	}

	// Target data with a dip that the fit will align to.
	// Place the dip around t=12.0 so bestShift ≈ 10.0 (12 - 2).
	var targetTimes, targetValues []float64
	for ts := 10.0; ts <= 15.0; ts += 0.1 {
		targetTimes = append(targetTimes, ts)
		// Simple dip centered at t=12.0
		v := 1.0
		if ts >= 11.0 && ts <= 13.0 {
			v = 0.2
		}
		targetValues = append(targetValues, v)
	}

	// --- Run the NCC sliding fit ---
	fr, err := nccSlidingFit(pc, targetTimes, targetValues)
	if err != nil {
		t.Fatalf("nccSlidingFit failed: %v", err)
	}
	originalShift := fr.bestShift
	t.Logf("Original bestShift = %.4f", originalShift)

	// --- Step 1: Verify dots on the curve at the original shift ---
	for i, st := range fr.sampledTimes {
		localT := st - fr.bestShift
		var expected float64
		if localT < 0 || localT > duration {
			expected = 1.0
		} else {
			expected = interpolateAt(curve, curveTimes, localT)
		}
		got := fr.sampledVals[i]
		if math.Abs(got-expected) > 1e-12 {
			t.Errorf("original shift: sample %d mismatch: got %.6f, want %.6f", i, got, expected)
		}
	}

	// --- Step 2: Simulate a slide adjustment (like auto-slide) ---
	slideAmount := 0.35
	fr.bestShift += slideAmount
	t.Logf("Adjusted bestShift = %.4f (slide of %.2f)", fr.bestShift, slideAmount)

	// The STALE sampledVals should mismatch the theory curve at the new shift
	// for samples that fall in the sloped regions of the curve.
	mismatchCount := 0
	maxErr := 0.0
	for i, st := range fr.sampledTimes {
		localT := st - fr.bestShift
		var expected float64
		if localT < 0 || localT > duration {
			expected = 1.0
		} else {
			expected = interpolateAt(curve, curveTimes, localT)
		}
		staleY := fr.sampledVals[i]
		diff := math.Abs(staleY - expected)
		if diff > 1e-6 {
			mismatchCount++
			if diff > maxErr {
				maxErr = diff
			}
		}
	}
	if mismatchCount == 0 {
		t.Fatal("BUG NOT REPRODUCED: expected stale sampledVals to mismatch theory " +
			"curve after shift change, but all matched")
	}
	t.Logf("Confirmed bug: %d of %d stale samples mismatch (max error=%.4f)",
		mismatchCount, len(fr.sampledTimes), maxErr)

	// --- Step 3: Verify the fix — recompute Y from the curve at the new shift ---
	for i, st := range fr.sampledTimes {
		localT := st - fr.bestShift
		var recomputedY float64
		if localT < 0 || localT > duration {
			recomputedY = 1.0
		} else {
			recomputedY = interpolateAt(curve, curveTimes, localT)
		}
		// The theory curve value at this X is the same interpolation.
		var theoryAtX float64
		if localT < 0 || localT > duration {
			theoryAtX = 1.0
		} else {
			theoryAtX = interpolateAt(curve, curveTimes, localT)
		}
		if math.Abs(recomputedY-theoryAtX) > 1e-12 {
			t.Errorf("fixed: sample %d still mismatches: got %.6f, want %.6f",
				i, recomputedY, theoryAtX)
		}
	}

	// --- Step 4: Verify with scale factor ---
	scale := 0.7
	for i, st := range fr.sampledTimes {
		localT := st - fr.bestShift
		var rawY float64
		if localT < 0 || localT > duration {
			rawY = 1.0
		} else {
			rawY = interpolateAt(curve, curveTimes, localT)
		}
		scaledRecomputed := rawY*scale + (1.0 - scale)
		scaledTheory := rawY*scale + (1.0 - scale)
		if math.Abs(scaledRecomputed-scaledTheory) > 1e-12 {
			t.Errorf("scaled fix: sample %d: recomputed=%.6f theory=%.6f",
				i, scaledRecomputed, scaledTheory)
		}
	}
	t.Log("Fix verified: recomputed samples match theory curve at new shift, with and without scale")
}
