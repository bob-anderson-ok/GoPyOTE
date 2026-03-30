package main


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

