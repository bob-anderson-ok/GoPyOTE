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

// fitPolynomial fits a polynomial of the given degree to (x, y) data using
// least-squares (normal equations solved via Gaussian elimination).
// Returns the coefficients [c0, c1, c2, ...] where y = c0 + c1*x + c2*x² + ...
func fitPolynomial(x, y []float64, degree int) ([]float64, error) {
	n := len(x)
	if n != len(y) {
		return nil, errors.New("x and y length mismatch")
	}
	if n <= degree {
		return nil, fmt.Errorf("need at least %d points for degree-%d polynomial", degree+1, degree)
	}

	p := degree + 1

	// Center and scale x to improve numerical conditioning.
	xMean := 0.0
	for _, v := range x {
		xMean += v
	}
	xMean /= float64(n)

	xScale := 0.0
	for _, v := range x {
		d := math.Abs(v - xMean)
		if d > xScale {
			xScale = d
		}
	}
	if xScale == 0 {
		xScale = 1.0
	}

	xs := make([]float64, n)
	for i, v := range x {
		xs[i] = (v - xMean) / xScale
	}

	// Build normal equations: (X^T X) a = X^T y
	XTX := make([][]float64, p)
	for i := range XTX {
		XTX[i] = make([]float64, p)
	}
	XTy := make([]float64, p)

	for i := 0; i < n; i++ {
		powers := make([]float64, p)
		powers[0] = 1.0
		for k := 1; k < p; k++ {
			powers[k] = powers[k-1] * xs[i]
		}
		for r := 0; r < p; r++ {
			XTy[r] += powers[r] * y[i]
			for c := 0; c < p; c++ {
				XTX[r][c] += powers[r] * powers[c]
			}
		}
	}

	scaledCoeffs, err := solveLinearSystem(XTX, XTy)
	if err != nil {
		return nil, fmt.Errorf("polynomial fit: %w", err)
	}

	// Convert coefficients back to the original x domain.
	// The scaled polynomial is: y = Σ a_k * ((x - xMean)/xScale)^k
	// Expand to get coefficients in the original x domain.
	// Use binomial expansion: ((x - xMean)/xScale)^k = Σ C(k,j) * x^j * (-xMean)^(k-j) / xScale^k
	// This is complex, so instead store xMean and xScale with the coefficients
	// and evaluate using evalPolynomial.
	// Return scaled coefficients with xMean, xScale encoded as the last two elements.
	result := make([]float64, p+2)
	copy(result, scaledCoeffs)
	result[p] = xMean
	result[p+1] = xScale
	return result, nil
}

// evalPolynomial evaluates a polynomial (from fitPolynomial) at the given x value.
// coeffs has p coefficients followed by xMean and xScale.
func evalPolynomial(coeffs []float64, x float64) float64 {
	p := len(coeffs) - 2
	xMean := coeffs[p]
	xScale := coeffs[p+1]
	xs := (x - xMean) / xScale

	result := coeffs[p-1]
	for k := p - 2; k >= 0; k-- {
		result = result*xs + coeffs[k]
	}
	return result
}

// detrendPolynomial subtracts a degree-3 polynomial trend from baseline data
// using the actual X positions. Returns the detrended residuals, the polynomial
// coefficients (for plotting), and the detrended sigma.
func detrendPolynomial(xPositions, yValues []float64) (residuals []float64, coeffs []float64, sigma float64, err error) {
	n := len(xPositions)
	if n != len(yValues) {
		return nil, nil, 0, errors.New("x and y length mismatch")
	}
	if n < 5 {
		return nil, nil, 0, fmt.Errorf("need at least 5 points for polynomial detrending, got %d", n)
	}

	coeffs, err = fitPolynomial(xPositions, yValues, 3)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("polynomial fit failed: %w", err)
	}

	residuals = make([]float64, n)
	for i := range yValues {
		residuals[i] = yValues[i] - evalPolynomial(coeffs, xPositions[i])
	}

	sigma = stddev(residuals)
	return residuals, coeffs, sigma, nil
}

// computeBaselineTrend computes a Savitzky-Golay trend line from baseline data
// using the given window size and returns it. Also prints detrending diagnostics.
// The window must be odd and > 3; the caller is responsible for computing it
// (e.g., from sample rate × 5 seconds).
func computeBaselineTrend(data []float64, window int) ([]float64, error) {
	if len(data) < 5 {
		return nil, fmt.Errorf("need at least 5 baseline points for trend computation")
	}

	if window%2 == 0 {
		window += 1
	}
	if window <= 3 {
		window = 5
	}
	if window > len(data) {
		window = len(data)
		if window%2 == 0 {
			window -= 1
		}
	}
	degree := 3

	trend, err := savitzkyGolaySmooth(data, window, degree)
	if err != nil {
		return nil, fmt.Errorf("savitzkyGolaySmooth: %w", err)
	}

	noise, err := detrend(data, trend)
	if err != nil {
		return nil, fmt.Errorf("detrend: %w", err)
	}

	sd := stddev(noise)

	lagCoeffs, err := autocorrCoeffs(noise, 10)
	if err != nil {
		fmt.Printf("Savitzky-Golay detrending (autocorr failed: %v)\n", err)
		return trend, nil
	}

	fmt.Printf("\nAfter Savitzky-Golay detrending (degree=%d, window=%d, sigma=%.10f):\n", degree, window, sd)
	fmt.Println("Lag autocorrelation coefficients:")
	for i, v := range lagCoeffs {
		fmt.Printf("  lag %d: %.10f\n", i+1, v)
	}

	return trend, nil
}
