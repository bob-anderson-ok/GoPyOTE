# How Monte Carlo Trials Estimate Edge Time Uncertainties

## 

## 1. Initial Fit

## 

A best-fit is found by sliding the theoretical diffraction light curve against the observed data using Normalized Cross-Correlation (NCC). This determines:

## 

- The time shift that best aligns the theoretical curve to the observed data
- The edge times (D and R) from the theoretical curve at that alignment
- The sampled theoretical values at each observed data point's timestamp

## 

## 2. Baseline Noise Measurement

## 

The user normalizes the baseline, which measures `noiseSigma` — the standard deviation of the observed light curve's baseline noise. During this process, the lag coefficients out to lag 10 are calculated and saved. These coefficients characterize the degree of correlation, caused by scintillation, that exists in the observation noise. These values are essential and are used to generate realistic noise samples for later operations.

## 

## 3. Monte Carlo Resampling

## 

Each trial:

## 

1. Takes the **sampled theoretical curve** from the best fit (the "true" signal)
2. Adds **Gaussian noise** scaled by both `noiseSigma` and the signal value: `v + rand.NormFloat64() * noiseSigma * v` — brighter points get proportionally more noise.
3. The noise from the above operation is white noise. This is converted to correlated noise using the measured lag coefficients.
4. This creates a realistic synthetic  "noisy observation"

## 

## 4. Refitting

## 

Each noisy synthetic observation is re-fit against some set of precomputed candidate curves (across path offsets) using the same NCC sliding fit. The best-matching candidate gives new edge times for that trial.

## 

## 5. Statistics

## 

After all trials complete:

## 

- The **mean** and **standard deviation** of each edge time are computed across all trials
- When there are 2 edges, both sigmas are set to the **maximum** of the two (conservative estimate)
- The **duration uncertainty** is calculated as `sqrt(sigmaD² + sigmaR²)`, treating edge errors as independent
- Results are reported as **±3σ** (99.7% confidence interval)

## 

The key insight: by repeatedly adding realistic noise to the best-fit theoretical curve and re-fitting, the spread of recovered edge times directly measures how much noise-induced uncertainty exists in the edge time estimates.

## 
