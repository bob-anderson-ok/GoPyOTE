package main

import "sort"

// timeIntensityPoint holds a single time-intensity pair from a light curve.
type timeIntensityPoint struct {
	time      float64
	intensity float64
}

// applyCameraExposure integrates the theoretical light curve over a camera
// exposure window. For each point at time t, the output intensity is the
// average of the curve from t to t+exposureTimeSecs (trapezoidal rule).
// Returns a new slice with the same time values but integrated intensities.
// No-op if exposureTimeSecs <= 0.
func applyCameraExposure(curve []timeIntensityPoint, exposureTimeSecs float64) []timeIntensityPoint {
	if exposureTimeSecs <= 0 || len(curve) == 0 {
		return curve
	}

	// Extract sorted time values for binary search
	times := make([]float64, len(curve))
	for i, pt := range curve {
		times[i] = pt.time
	}

	result := make([]timeIntensityPoint, len(curve))
	for i, pt := range curve {
		tStart := pt.time
		tEnd := tStart + exposureTimeSecs
		avg := integrateTrapezoidal(curve, times, tStart, tEnd) / exposureTimeSecs
		result[i] = timeIntensityPoint{time: pt.time, intensity: avg}
	}
	return result
}

// integrateTrapezoidal computes the integral of the curve from tStart to tEnd
// using the trapezoidal rule over the curve's own high-resolution points.
func integrateTrapezoidal(curve []timeIntensityPoint, times []float64, tStart, tEnd float64) float64 {
	if tStart >= tEnd {
		return 0
	}

	// Find the index of the first curve point >= tStart
	iStart := sort.SearchFloat64s(times, tStart)
	// Find the index of the first curve point > tEnd
	iEnd := sort.SearchFloat64s(times, tEnd)

	// Collect the sub-points within [tStart, tEnd] including interpolated boundaries
	// Start with the interpolated value at tStart
	integral := 0.0
	prevT := tStart
	prevV := interpolateAt(curve, times, tStart)

	// Add contributions from each curve point inside the window
	for i := iStart; i < iEnd && i < len(curve); i++ {
		t := curve[i].time
		if t <= tStart {
			continue
		}
		if t >= tEnd {
			break
		}
		v := curve[i].intensity
		integral += (t - prevT) * (prevV + v) / 2
		prevT = t
		prevV = v
	}

	// Final segment to tEnd
	endV := interpolateAt(curve, times, tEnd)
	integral += (tEnd - prevT) * (prevV + endV) / 2

	return integral
}

// interpolateAt returns the linearly interpolated intensity at an arbitrary
// time t. Clamps to boundary values if t is outside the curve range.
func interpolateAt(curve []timeIntensityPoint, times []float64, t float64) float64 {
	if len(curve) == 0 {
		return 0
	}
	if t <= curve[0].time {
		return curve[0].intensity
	}
	if t >= curve[len(curve)-1].time {
		return curve[len(curve)-1].intensity
	}

	// Find the insertion point: times[i-1] <= t < times[i]
	i := sort.SearchFloat64s(times, t)
	if i >= len(curve) {
		return curve[len(curve)-1].intensity
	}
	if i == 0 {
		return curve[0].intensity
	}

	t0 := curve[i-1].time
	t1 := curve[i].time
	v0 := curve[i-1].intensity
	v1 := curve[i].intensity
	if t1 == t0 {
		return v0
	}
	return v0 + (v1-v0)*(t-t0)/(t1-t0)
}
