package main

import (
	"math"
	"testing"
)

// TestRefineShiftAndScaleCoarseSampling reproduces the block-integration case: a short
// dip sampled at a long period, with the true alignment falling between the whole-sample
// shifts the NCC search tries. The drop scale must be re-fitted as the shift is refined;
// fitting it once at the NCC shift gives the wrong depth.
func TestRefineShiftAndScaleCoarseSampling(t *testing.T) {
	// Triangular dip reaching 0 at t=2, 0.8 s wide (a 0.4 s event smoothed by a 0.4 s exposure)
	var curve []timeIntensityPoint
	for tt := 0.0; tt <= 4.0+1e-9; tt += 0.005 {
		curve = append(curve, timeIntensityPoint{time: tt, intensity: math.Min(1, math.Abs(tt-2)/0.4)})
	}
	curveTimes := make([]float64, len(curve))
	for i, pt := range curve {
		curveTimes[i] = pt.time
	}
	pc := &precomputedCurve{curve: curve, curveTimes: curveTimes, duration: curve[len(curve)-1].time}

	const trueShift, trueScale, period = 10.13, 0.6, 0.4
	var times, values []float64
	for i := 0; i < 40; i++ {
		ts := 8.0 + float64(i)*period
		times = append(times, ts)
		theory := sampleTheoryAtShift(pc, []float64{ts}, trueShift)[0]
		values = append(values, theory*trueScale+(1-trueScale))
	}

	fr, err := nccSlidingFit(pc, times, values)
	if err != nil {
		t.Fatal(err)
	}
	nccScale, _, _ := findBestScale(fr.sampledVals, values)
	t.Logf("NCC shift=%.4f, scale fitted there=%.2f", fr.bestShift, nccScale)

	refineShiftAndScale(fr, pc, times, values)
	scale, mse, _ := findBestScale(fr.sampledVals, values)
	t.Logf("refined shift=%.4f, scale=%.2f, MSE=%.2g", fr.bestShift, scale, mse)

	if math.Abs(fr.bestShift-trueShift) > 0.021 {
		t.Errorf("refined shift = %.4f, want %.2f ± one step", fr.bestShift, trueShift)
	}
	if math.Abs(scale-trueScale) > 0.05 {
		t.Errorf("refined scale = %.2f, want %.2f", scale, trueScale)
	}
}

// boxEventCurve returns a 4 s theoretical curve with a full-depth event of the given
// duration centred at t=2, smoothed by the camera exposure.
func boxEventCurve(pathOffset, duration, exposure float64) *precomputedCurve {
	var curve []timeIntensityPoint
	for tt := 0.0; tt <= 4.0+1e-9; tt += 0.002 {
		v := 1.0
		if math.Abs(tt-2) < duration/2 {
			v = 0
		}
		curve = append(curve, timeIntensityPoint{time: tt, intensity: v})
	}
	curve = applyCameraExposure(curve, exposure)
	curveTimes := make([]float64, len(curve))
	for i, pt := range curve {
		curveTimes[i] = pt.time
	}
	return &precomputedCurve{pathOffset: pathOffset, curve: curve, curveTimes: curveTimes, duration: curve[len(curve)-1].time}
}

// TestSelectByScaledMSEPrefersMatchingDepth reproduces a single-point event in block
// integrated data: a graze (0.094 s) and a more central chord (0.25 s), each smoothed by
// a 0.32 s exposure, give the same one-low-sample shape, so NCC cannot tell them apart.
// The observed point is deeper than the graze can reach; selection by MSE after the
// depth fit must pick the central chord.
func TestSelectByScaledMSEPrefersMatchingDepth(t *testing.T) {
	const exposure = 0.32
	graze := boxEventCurve(1.93, 0.094, exposure)
	central := boxEventCurve(1.5, 0.25, exposure)

	var times, values []float64
	for i := 0; i < 40; i++ {
		ts := 8.0 + float64(i)*exposure
		times = append(times, ts)
		values = append(values, sampleTheoryAtShift(central, []float64{ts}, 10.05)[0])
	}

	var results []searchResult
	for _, pc := range []*precomputedCurve{graze, central} {
		fr, err := nccSlidingFit(pc, times, values)
		if err != nil {
			t.Fatal(err)
		}
		results = append(results, searchResult{pathOffset: pc.pathOffset, peakOverlapNCC: fr.bestOverlapNCC, fr: fr, pc: pc})
	}
	t.Logf("overlap NCC: graze=%.4f central=%.4f", results[0].peakOverlapNCC, results[1].peakOverlapNCC)

	best := selectByScaledMSE(results, times, values)
	t.Logf("MSE after depth fit: graze=%.6f (scale %.2f), central=%.6f (scale %.2f)",
		results[0].mse, results[0].fr.bestScale, results[1].mse, results[1].fr.bestScale)
	if best != 1 {
		t.Errorf("selected path offset %.2f km, want the central chord (1.50 km)", results[best].pathOffset)
	}
}
