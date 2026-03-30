package main

import (
	"fmt"
	"math"
	"math/rand"
	"time"
)

//
// Fit AR model from autocorrelation (Yule-Walker)
//

func fitARFromACF(rho []float64, p int) ([]float64, float64, error) {

	if len(rho) < p+1 {
		return nil, 0, fmt.Errorf("need rho[0..p]")
	}

	R := make([][]float64, p)
	r := make([]float64, p)

	for i := 0; i < p; i++ {

		R[i] = make([]float64, p)

		for j := 0; j < p; j++ {

			lag := i - j
			if lag < 0 {
				lag = -lag
			}

			R[i][j] = rho[lag]
		}

		r[i] = rho[i+1]
	}

	phi, err := solveLinearSystem(R, r)

	if err != nil {
		return nil, 0, err
	}

	// innovation variance

	sigma2 := 1.0

	for j := 0; j < p; j++ {
		sigma2 -= phi[j] * rho[j+1]
	}

	if sigma2 <= 0 {
		return nil, 0, fmt.Errorf("invalid AR model")
	}

	return phi, sigma2, nil
}

//
// Generate AR noise
//

func generateAR(n int, phi []float64, sigma2 float64, rng *rand.Rand) []float64 {

	p := len(phi)

	// Use a burn-in period so the AR process reaches its stationary variance
	// before we start collecting output samples. Without this, the first p
	// samples have reduced variance because the AR feedback terms are missing,
	// which attenuates the effective noise amplitude.
	burnIn := 10 * p
	total := burnIn + n
	x := make([]float64, total)

	sigma := math.Sqrt(sigma2)

	for t := 0; t < total; t++ {

		val := sigma * rng.NormFloat64()

		for j := 0; j < p; j++ {

			if t-1-j >= 0 {
				val += phi[j] * x[t-1-j]
			}
		}

		x[t] = val
	}

	return x[burnIn:]
}

//
// Example usage
//

func testARmethod(rho []float64) {

	// rho is the measured autocorrelation
	// rho[0] must equal 1

	p := 10

	phi, sigma2, err := fitARFromACF(rho, p)

	if err != nil {
		panic(err)
	}

	fmt.Println("AR coefficients")

	for i, v := range phi {
		fmt.Printf("phi[%d] = %.6f\n", i+1, v)
	}

	fmt.Printf("\ninnovation variance = %.6f\n\n", sigma2)

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	x := generateAR(200000, phi, sigma2, rng)

	fmt.Printf("Standard deviation of simulated AR noise = %.10f\n", stddev(x))

	acf := sampleACF(x, 10)

	fmt.Println("Target vs simulated ACF")

	for i := 0; i <= 10; i++ {

		fmt.Printf(
			"lag %2d   target %.5f   sim %.5f\n",
			i,
			rho[i],
			acf[i],
		)
	}
}
