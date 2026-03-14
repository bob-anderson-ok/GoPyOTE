package main

import (
	"fmt"
	"math"
	"math/rand"
)

type FitResult struct {
	Coeffs     []float64 // normalized coefficients
	RawCoeffs  []float64 // unnormalized with c0=1
	Error      float64
	TheoryACF  []float64
	ModelOrder int
}

// normalizeCoeffs rescales coefficients so that the output variance is 1
// when the white input noise has variance 1.
func normalizeCoeffs(coeffs []float64) []float64 {
	sumSq := 0.0
	for _, c := range coeffs {
		sumSq += c * c
	}
	scale := 1.0 / math.Sqrt(sumSq)

	out := make([]float64, len(coeffs))
	for i, c := range coeffs {
		out[i] = c * scale
	}
	return out
}

// theoreticalMAACF returns the theoretical autocorrelation for an MA(q)
// with coefficients coeffs, assuming white-noise variance = 1.
func theoreticalMAACF(coeffs []float64, maxLag int) []float64 {
	q := len(coeffs) - 1
	rho := make([]float64, maxLag+1)

	gamma0 := 0.0
	for _, c := range coeffs {
		gamma0 += c * c
	}

	for lag := 0; lag <= maxLag; lag++ {
		if lag > q {
			rho[lag] = 0.0
			continue
		}
		gamma := 0.0
		for j := 0; j <= q-lag; j++ {
			gamma += coeffs[j] * coeffs[j+lag]
		}
		rho[lag] = gamma / gamma0
	}

	return rho
}

// weightedACFError computes a weighted squared error.
// Usually you care more about small lags, so weights[k] can decrease with k.
func weightedACFError(modelACF, targetACF, weights []float64) float64 {
	n := len(targetACF)
	err := 0.0
	for k := 1; k < n; k++ { // skip lag 0 since it is always 1
		w := 1.0
		if weights != nil && k < len(weights) {
			w = weights[k]
		}
		d := modelACF[k] - targetACF[k]
		err += w * d * d
	}
	return err
}

// fitMA3Grid searches MA(2): c0=1, c1, c2.
func fitMA3Grid(targetACF []float64, c1Min, c1Max, c2Min, c2Max float64, steps int, weights []float64) FitResult {
	best := FitResult{Error: math.Inf(1), ModelOrder: 2}
	maxLag := len(targetACF) - 1

	for i := 0; i <= steps; i++ {
		c1 := c1Min + (c1Max-c1Min)*float64(i)/float64(steps)
		for j := 0; j <= steps; j++ {
			c2 := c2Min + (c2Max-c2Min)*float64(j)/float64(steps)

			raw := []float64{1.0, c1, c2}
			norm := normalizeCoeffs(raw)
			model := theoreticalMAACF(norm, maxLag)
			err := weightedACFError(model, targetACF, weights)

			if err < best.Error {
				best = FitResult{
					Coeffs:     norm,
					RawCoeffs:  raw,
					Error:      err,
					TheoryACF:  model,
					ModelOrder: 2,
				}
			}
		}
	}

	return best
}

// fitMA4Grid searches MA(3): c0=1, c1, c2, c3.
func fitMA4Grid(targetACF []float64, c1Min, c1Max, c2Min, c2Max, c3Min, c3Max float64, steps int, weights []float64) FitResult {
	best := FitResult{Error: math.Inf(1), ModelOrder: 3}
	maxLag := len(targetACF) - 1

	for i := 0; i <= steps; i++ {
		c1 := c1Min + (c1Max-c1Min)*float64(i)/float64(steps)
		for j := 0; j <= steps; j++ {
			c2 := c2Min + (c2Max-c2Min)*float64(j)/float64(steps)
			for k := 0; k <= steps; k++ {
				c3 := c3Min + (c3Max-c3Min)*float64(k)/float64(steps)

				raw := []float64{1.0, c1, c2, c3}
				norm := normalizeCoeffs(raw)
				model := theoreticalMAACF(norm, maxLag)
				err := weightedACFError(model, targetACF, weights)

				if err < best.Error {
					best = FitResult{
						Coeffs:     norm,
						RawCoeffs:  raw,
						Error:      err,
						TheoryACF:  model,
						ModelOrder: 3,
					}
				}
			}
		}
	}

	return best
}

// generateWhiteNoise returns N(0,1) samples.
func generateWhiteNoise(n int, rng *rand.Rand) []float64 {
	x := make([]float64, n)
	for i := range x {
		x[i] = rng.NormFloat64()
	}
	return x
}

// applyMAFilter computes
// y[t] = coeffs[0]*w[t] + coeffs[1]*w[t-1] + ...
func applyMAFilter(w []float64, coeffs []float64) []float64 {
	n := len(w)
	q := len(coeffs) - 1
	y := make([]float64, n)

	for t := 0; t < n; t++ {
		sum := 0.0
		for j := 0; j <= q; j++ {
			idx := t - j
			if idx >= 0 {
				sum += coeffs[j] * w[idx]
			}
		}
		y[t] = sum
	}
	return y
}

// sampleACF estimates the autocorrelation up to maxLag.
func sampleACF(x []float64, maxLag int) []float64 {
	n := len(x)
	if maxLag >= n {
		maxLag = n - 1
	}

	mean := 0.0
	for _, v := range x {
		mean += v
	}
	mean /= float64(n)

	var0 := 0.0
	for _, v := range x {
		d := v - mean
		var0 += d * d
	}
	var0 /= float64(n)

	rho := make([]float64, maxLag+1)
	for lag := 0; lag <= maxLag; lag++ {
		cov := 0.0
		for i := 0; i < n-lag; i++ {
			cov += (x[i] - mean) * (x[i+lag] - mean)
		}
		cov /= float64(n - lag)
		rho[lag] = cov / var0
	}
	return rho
}

func printFit(name string, fit FitResult, target []float64) {
	fmt.Printf("\n%s best fit: MA(%d)\n", name, fit.ModelOrder)
	fmt.Printf("raw coeffs: ")
	for i, c := range fit.RawCoeffs {
		if i > 0 {
			fmt.Printf(", ")
		}
		fmt.Printf("%.6f", c)
	}
	fmt.Printf("\nnormalized coeffs: ")
	for i, c := range fit.Coeffs {
		if i > 0 {
			fmt.Printf(", ")
		}
		fmt.Printf("%.6f", c)
	}
	fmt.Printf("\nweighted error: %.8f\n", fit.Error)

	fmt.Println("lag   target      model")
	for k := 0; k < len(target); k++ {
		fmt.Printf("%2d   %.6f   %.6f\n", k, target[k], fit.TheoryACF[k])
	}
}

// testCorrNoise is the original MA-based correlated noise test (kept for reference).
// Use the testARmethod (in ARcorrelatedNoise.go) instead.
//func testCorrNoise() { ... }
