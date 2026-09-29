package main

import (
	"math"
	"testing"
)

// TestScaledCurveDrop checks that the reported drop reflects the depth of the scaled
// theoretical curve, not just the scale factor.
func TestScaledCurveDrop(t *testing.T) {
	shallow := []timeIntensityPoint{{0, 1}, {1, 0.5}, {2, 0.15}, {3, 0.5}, {4, 1}}
	deep := []timeIntensityPoint{{0, 1}, {1, 0}, {2, 0}, {3, 1}}

	tests := []struct {
		name  string
		curve []timeIntensityPoint
		scale float64
		want  float64
	}{
		{"shallow curve, full scale (single-point case)", shallow, 1.0, 0.85},
		{"shallow curve, partial scale", shallow, 0.5, 0.425},
		{"deep curve, full scale", deep, 1.0, 1.0},
		{"deep curve, partial scale", deep, 0.6, 0.6},
		{"scale zero", shallow, 0, 0},
		{"empty curve", nil, 1.0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := scaledCurveDrop(tc.curve, tc.scale); math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("scaledCurveDrop = %v, want %v", got, tc.want)
			}
		})
	}
}
