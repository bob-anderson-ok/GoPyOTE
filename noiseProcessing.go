package main

import (
	"errors"
	"fmt"
	"math"
)

// reflectIndex mirrors an index into the valid range [0, n-1].
func reflectIndex(i, n int) int {
	if n <= 1 {
		return 0
	}
	for i < 0 || i >= n {
		if i < 0 {
			i = -i - 1
		}
		if i >= n {
			i = 2*n - i - 1
		}
	}
	return i
}

// solveLinearSystem solves A x = b using Gaussian elimination
// with partial pivoting.
func solveLinearSystem(A [][]float64, b []float64) ([]float64, error) {
	n := len(b)
	if len(A) != n {
		return nil, errors.New("matrix/vector size mismatch")
	}
	for i := range A {
		if len(A[i]) != n {
			return nil, errors.New("matrix must be square")
		}
	}

	// Build augmented matrix
	M := make([][]float64, n)
	for i := 0; i < n; i++ {
		M[i] = make([]float64, n+1)
		copy(M[i], A[i])
		M[i][n] = b[i]
	}

	// Forward elimination
	for k := 0; k < n; k++ {
		// Pivot
		pivotRow := k
		pivotVal := math.Abs(M[k][k])
		for i := k + 1; i < n; i++ {
			if v := math.Abs(M[i][k]); v > pivotVal {
				pivotVal = v
				pivotRow = i
			}
		}
		if pivotVal < 1e-15 {
			return nil, errors.New("singular or ill-conditioned system")
		}
		M[k], M[pivotRow] = M[pivotRow], M[k]

		// Eliminate
		for i := k + 1; i < n; i++ {
			f := M[i][k] / M[k][k]
			for j := k; j <= n; j++ {
				M[i][j] -= f * M[k][j]
			}
		}
	}

	// Back substitution
	x := make([]float64, n)
	for i := n - 1; i >= 0; i-- {
		sum := M[i][n]
		for j := i + 1; j < n; j++ {
			sum -= M[i][j] * x[j]
		}
		x[i] = sum / M[i][i]
	}

	return x, nil
}

// savitzkyGolaySmooth computes an SG-smoothed version of y.
// degree should be 3 for your use case.
// window must be odd and > degree.
func savitzkyGolaySmooth(y []float64, window, degree int) ([]float64, error) {
	n := len(y)
	if n == 0 {
		return nil, errors.New("empty input")
	}
	if window%2 == 0 || window < 1 {
		return nil, errors.New("window must be a positive odd integer")
	}
	if degree < 0 {
		return nil, errors.New("degree must be nonnegative")
	}
	if window <= degree {
		return nil, errors.New("window must be greater than degree")
	}

	half := window / 2
	p := degree + 1
	out := make([]float64, n)

	// For each output point, fit a local polynomial around x=0
	// and take the fitted value at x=0, which is the intercept term.
	for center := 0; center < n; center++ {
		// Build normal equations: (X^T X) a = X^T y
		XTX := make([][]float64, p)
		for i := range XTX {
			XTX[i] = make([]float64, p)
		}
		XTy := make([]float64, p)

		for j := -half; j <= half; j++ {
			idx := reflectIndex(center+j, n)
			x := float64(j)
			yy := y[idx]

			// powers[k] = x^k
			powers := make([]float64, p)
			powers[0] = 1.0
			for k := 1; k < p; k++ {
				powers[k] = powers[k-1] * x
			}

			for r := 0; r < p; r++ {
				XTy[r] += powers[r] * yy
				for c := 0; c < p; c++ {
					XTX[r][c] += powers[r] * powers[c]
				}
			}
		}

		coeffs, err := solveLinearSystem(XTX, XTy)
		if err != nil {
			return nil, err
		}

		// Fitted value at x=0 is the intercept.
		out[center] = coeffs[0]
	}

	return out, nil
}

// detrend subtracts trend from y.
func detrend(y, trend []float64) ([]float64, error) {
	if len(y) != len(trend) {
		return nil, errors.New("length mismatch in detrend")
	}
	out := make([]float64, len(y))
	for i := range y {
		out[i] = y[i] - trend[i]
	}
	return out, nil
}

// mean returns the arithmetic mean.
func mean(x []float64) float64 {
	if len(x) == 0 {
		return math.NaN()
	}
	sum := 0.0
	for _, v := range x {
		sum += v
	}
	return sum / float64(len(x))
}

// stddev returns the sample standard deviation.
func stddev(x []float64) float64 {
	n := len(x)
	if n < 2 {
		return 0
	}
	m := mean(x)
	ss := 0.0
	for _, v := range x {
		d := v - m
		ss += d * d
	}
	return math.Sqrt(ss / float64(n-1))
}

// autocorrCoeffs returns lag-1...lag-maxLag autocorrelation coefficients.
// It mean-centers x before computing them.
func autocorrCoeffs(x []float64, maxLag int) ([]float64, error) {
	n := len(x)
	if n < 2 {
		return nil, errors.New("need at least 2 samples")
	}
	if maxLag < 1 || maxLag >= n {
		return nil, errors.New("invalid maxLag")
	}

	m := mean(x)

	den := 0.0
	for _, v := range x {
		d := v - m
		den += d * d
	}
	if den == 0 {
		return nil, errors.New("zero variance")
	}

	rho := make([]float64, maxLag)
	for lag := 1; lag <= maxLag; lag++ {
		num := 0.0
		for i := 0; i < n-lag; i++ {
			num += (x[i] - m) * (x[i+lag] - m)
		}
		rho[lag-1] = num / den
	}

	return rho, nil
}

func testNoiseProcessing(data []float64) {
	// Replace this with your data.
	//data := []float64{
	//	0.12, 0.15, 0.20, 0.16, 0.21, 0.25, 0.31, 0.34, 0.36, 0.39,
	//	0.43, 0.45, 0.49, 0.53, 0.50, 0.47, 0.44, 0.40, 0.37, 0.35,
	//	0.33, 0.29, 0.25, 0.22, 0.18, 0.16, 0.14, 0.12, 0.11, 0.10,
	//	0.13, 0.12, 0.15, 0.17, 0.20, 0.24, 0.27, 0.30, 0.28, 0.26,
	//	0.23, 0.21, 0.19, 0.18, 0.16, 0.15, 0.14, 0.13, 0.12, 0.11,
	//}

	// SG settings: degree 3, odd window > degree.
	// Typical starting values: 7, 9, 11, 15 ...
	window := len(data) / 3
	if window%2 == 0 {
		window += 1
	}
	degree := 3

	trend, err := savitzkyGolaySmooth(data, window, degree)
	if err != nil {
		panic(err)
	}

	noise, err := detrend(data, trend)
	if err != nil {
		panic(err)
	}

	sd := stddev(noise)

	lagCoeffs, err := autocorrCoeffs(noise, 5)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Savitzky-Golay detrending\n")
	fmt.Printf("  degree = %d\n", degree)
	fmt.Printf("  window = %d\n\n", window)

	fmt.Printf("Standard deviation of detrended noise = %.10f\n\n", sd)

	fmt.Println("First 5 lag autocorrelation coefficients:")
	for i, v := range lagCoeffs {
		fmt.Printf("  lag %d: %.10f\n", i+1, v)
	}
}
