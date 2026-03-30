package main

import (
	"errors"
	"math"
)

// singularMatrixThreshold is the pivot threshold below which a matrix is
// considered singular during Gaussian elimination.
const singularMatrixThreshold = 1e-15

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
		if pivotVal < singularMatrixThreshold {
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


