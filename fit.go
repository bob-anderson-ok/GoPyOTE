package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"

	"GoPyOTE/lightcurve"

	"github.com/KevinWang15/go-json5"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
	"gonum.org/v1/plot/vg/vgimg"
)

// reverseNorm is a plot.Normalizer that maps [min,max] to [1,0] instead of [0,1],
// producing a reversed (right-to-left) axis when used as plt.X.Scale.
type reverseNorm struct{}

func (reverseNorm) Normalize(min, max, x float64) float64 {
	return (max - x) / (max - min)
}

// nccResult holds a single time-offset and its NCC scores.
type nccResult struct {
	offset       float64
	ncc          float64
	weightedNCC  float64
	overlapCount int
}

// computeNCC computes the normalized cross-correlation between two equal-length slices.
// Returns 0 if either signal is constant (zero variance).
func computeNCC(target, sampled []float64) float64 {
	n := len(target)
	if n == 0 || len(sampled) != n {
		return 0
	}

	// Compute means
	var sumX, sumY float64
	for i := 0; i < n; i++ {
		sumX += target[i]
		sumY += sampled[i]
	}
	meanX := sumX / float64(n)
	meanY := sumY / float64(n)

	// Compute NCC components
	var num, denomX, denomY float64
	for i := 0; i < n; i++ {
		dx := target[i] - meanX
		dy := sampled[i] - meanY
		num += dx * dy
		denomX += dx * dx
		denomY += dy * dy
	}

	denom := math.Sqrt(denomX * denomY)
	if denom == 0 {
		return 0
	}
	return num / denom
}

// fitResult holds the output of a single-offset NCC fit for reuse.
type fitResult struct {
	curve        []timeIntensityPoint
	edgeTimes    []float64
	nccCurve     []nccResult
	bestNCC      float64
	bestShift    float64
	sampledTimes []float64
	sampledVals  []float64
}

// precomputedCurve holds a theoretical light curve and edge times for a specific path offset,
// ready for NCC sliding fit without regenerating from the diffraction model.
type precomputedCurve struct {
	pathOffset float64
	curve      []timeIntensityPoint
	curveTimes []float64
	edgeTimes  []float64
	duration   float64
}

// buildPrecomputedCurve generates the theoretical light curve for the given params.
func buildPrecomputedCurve(params *OccultationParameters) (*precomputedCurve, error) {
	lcData, edges, err := lightcurve.ExtractAndPlotLightCurve(
		nil,
		params.DXKmPerSec,
		params.DYKmPerSec,
		params.PathPerpendicularOffsetKm,
		params.FundamentalPlaneWidthKm,
		params.FundamentalPlaneWidthNumPoints,
		filepath.Join(appDir, "targetImage16bit.png"),
		filepath.Join(appDir, "geometricShadow.png"),
		"",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to extract diffraction light curve: %w", err)
	}
	if len(lcData) == 0 {
		return nil, fmt.Errorf("no diffraction light curve data extracted")
	}

	shadowSpeed := math.Sqrt(params.DXKmPerSec*params.DXKmPerSec + params.DYKmPerSec*params.DYKmPerSec)
	if shadowSpeed == 0 {
		return nil, fmt.Errorf("shadow speed is zero — check dX and dY parameters")
	}

	kmStart := lcData[0].Distance
	curve := make([]timeIntensityPoint, len(lcData))
	for i, pt := range lcData {
		curve[i] = timeIntensityPoint{
			time:      (pt.Distance - kmStart) / shadowSpeed,
			intensity: pt.Intensity,
		}
	}

	curve = applyCameraExposure(curve, params.ExposureTimeSecs)

	distancePerPoint := params.FundamentalPlaneWidthKm / float64(params.FundamentalPlaneWidthNumPoints)
	edgeTimes := make([]float64, len(edges))
	for i, edge := range edges {
		edgeKm := edge * distancePerPoint
		edgeTimes[i] = (edgeKm - kmStart) / shadowSpeed
	}

	curveTimes := make([]float64, len(curve))
	for i, pt := range curve {
		curveTimes[i] = pt.time
	}

	return &precomputedCurve{
		pathOffset: params.PathPerpendicularOffsetKm,
		curve:      curve,
		curveTimes: curveTimes,
		edgeTimes:  edgeTimes,
		duration:   curve[len(curve)-1].time,
	}, nil
}

// nccSlidingFit runs the NCC sliding fit of a precomputed curve against target data.
func nccSlidingFit(pc *precomputedCurve, targetTimes, targetValues []float64) (*fitResult, error) {
	framePeriod := medianTimeDelta(targetTimes)
	if framePeriod <= 0 {
		return nil, fmt.Errorf("could not determine frame period from target timestamps")
	}

	shiftStart := targetTimes[0] - pc.duration
	shiftEnd := targetTimes[len(targetTimes)-1]

	numSteps := int((shiftEnd-shiftStart)/framePeriod) + 1
	results := make([]nccResult, 0, numSteps)
	sampled := make([]float64, len(targetTimes))

	for step := 0; step < numSteps; step++ {
		shift := shiftStart + float64(step)*framePeriod
		overlapCount := 0
		for i, t := range targetTimes {
			localT := t - shift
			if localT < 0 || localT > pc.duration {
				sampled[i] = 1.0
			} else {
				sampled[i] = interpolateAt(pc.curve, pc.curveTimes, localT)
				overlapCount++
			}
		}

		ncc := computeNCC(targetValues, sampled)
		results = append(results, nccResult{offset: shift, ncc: ncc, overlapCount: overlapCount})
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("no NCC results computed")
	}

	// Find max overlap count across all shifts
	maxOverlap := 0
	for _, r := range results {
		if r.overlapCount > maxOverlap {
			maxOverlap = r.overlapCount
		}
	}

	// Compute weighted NCC: w = overlapCount / maxOverlap, weightedNCC = ncc * w
	for i := range results {
		if maxOverlap > 0 && results[i].overlapCount > 0 {
			w := float64(results[i].overlapCount) / float64(maxOverlap)
			results[i].weightedNCC = results[i].ncc * w
		}
	}

	// Find the best shift by weighted NCC
	bestIdx := 0
	for i, r := range results {
		if r.weightedNCC > results[bestIdx].weightedNCC {
			bestIdx = i
		}
	}

	// Recompute the sampled theoretical values at the best shift
	bestShift := results[bestIdx].offset
	sampledVals := make([]float64, len(targetTimes))
	for i, t := range targetTimes {
		localT := t - bestShift
		if localT < 0 || localT > pc.duration {
			sampledVals[i] = 1.0
		} else {
			sampledVals[i] = interpolateAt(pc.curve, pc.curveTimes, localT)
		}
	}

	return &fitResult{
		curve:        pc.curve,
		edgeTimes:    pc.edgeTimes,
		nccCurve:     results,
		bestNCC:      results[bestIdx].weightedNCC,
		bestShift:    bestShift,
		sampledTimes: append([]float64{}, targetTimes...),
		sampledVals:  sampledVals,
	}, nil
}

// runSingleFit generates the theoretical curve for the given params and runs
// the NCC sliding fit against the target data. It returns the results without
// displaying anything.
//func runSingleFit(params *OccultationParameters, targetTimes, targetValues []float64) (*fitResult, error) {
//	pc, err := buildPrecomputedCurve(params)
//	if err != nil {
//		return nil, err
//	}
//	return nccSlidingFit(pc, targetTimes, targetValues)
//}

// displayFitResult shows the NCC plot, overlay plot, diffraction image, and edge times for a fit result.
// showDiagnostics gates in the NCC Fit Result window.
func displayFitResult(app fyne.App, w fyne.Window, params *OccultationParameters, fr *fitResult, targetTimes, targetValues []float64, showDiagnostics bool) error {
	if showDiagnostics {
		plotImg, err := createNCCPlotImage(fr.nccCurve, params.Title, 1000, 500)
		if err != nil {
			return fmt.Errorf("failed to create NCC plot: %w", err)
		}

		nccWindow := app.NewWindow("NCC Fit Result")
		plotCanvas := canvas.NewImageFromImage(plotImg)
		plotCanvas.FillMode = canvas.ImageFillOriginal
		nccWindow.SetContent(container.NewScroll(plotCanvas))
		nccWindow.Resize(fyne.NewSize(1050, 550))
		nccWindow.CenterOnScreen()
		nccWindow.Show()
	}

	fmt.Printf("Best NCC fit: offset=%.4f sec, NCC=%.6f\n", fr.bestShift, fr.bestNCC)

	overlayImg, err := createOverlayPlotImage(fr.curve, fr.bestShift, fr.edgeTimes, targetTimes, targetValues, fr.sampledTimes, fr.sampledVals, fr.bestNCC, params.Title, 1200, 500, nil)
	if err != nil {
		return fmt.Errorf("failed to create overlay plot: %w", err)
	}

	overlayWindow := app.NewWindow("Fit Result — Theoretical vs Observed")
	overlayCanvas := canvas.NewImageFromImage(overlayImg)
	overlayCanvas.FillMode = canvas.ImageFillContain
	overlayWindow.SetContent(overlayCanvas)
	overlayWindow.Resize(fyne.NewSize(1250, 550))
	overlayWindow.CenterOnScreen()
	overlayWindow.Show()

	displayImg, err := lightcurve.LoadImageFromFile(filepath.Join(appDir, "diffractionImage8bit.png"))
	if err != nil {
		fmt.Printf("Could not load diffractionImage8bit.png: %v\n", err)
	} else {
		path := &lightcurve.ObservationPath{
			DxKmPerSec:               params.DXKmPerSec,
			DyKmPerSec:               params.DYKmPerSec,
			PathOffsetFromCenterKm:   params.PathPerpendicularOffsetKm,
			FundamentalPlaneWidthKm:  params.FundamentalPlaneWidthKm,
			FundamentalPlaneWidthPts: params.FundamentalPlaneWidthNumPoints,
		}
		if err := path.ComputePathFromVelocity(); err != nil {
			fmt.Printf("Could not compute observation path: %v\n", err)
		} else {
			annotatedImg, err := lightcurve.DrawObservationLineOnImage(displayImg, path)
			if err != nil {
				fmt.Printf("Could not draw observation path: %v\n", err)
			} else {
				pathTitle := fmt.Sprintf("Observation Path on Diffraction Image (offset=%.3f km)", params.PathPerpendicularOffsetKm)
				if params.Title != "" {
					pathTitle = params.Title + " — " + pathTitle
				}
				// Save the observation path image to the results folder
				if resultsFolder != "" {
					var buf bytes.Buffer
					if err := png.Encode(&buf, annotatedImg); err == nil {
						savePath := filepath.Join(resultsFolder, "observationPath.png")
						if err := os.WriteFile(savePath, buf.Bytes(), 0644); err != nil {
							fmt.Printf("Warning: could not save observationPath.png: %v\n", err)
						}
					}
				}
				pathWindow := app.NewWindow(pathTitle)
				pathCanvas := canvas.NewImageFromImage(annotatedImg)
				pathCanvas.FillMode = canvas.ImageFillContain
				pathWindow.SetContent(container.NewScroll(pathCanvas))
				pathWindow.Resize(fyne.NewSize(600, 600))
				pathWindow.CenterOnScreen()
				pathWindow.Show()
			}
		}
	}

	// Display edge times in a popup dialog
	if len(fr.edgeTimes) > 0 {
		msg := fmt.Sprintf("Best fit: NCC=%.4f, time offset=%.4f sec\n", fr.bestNCC, fr.bestShift)
		msg += fmt.Sprintf("Path offset=%.3f km\n\n", params.PathPerpendicularOffsetKm)
		msg += "Edge times:\n"
		for i, et := range fr.edgeTimes {
			edgeAbsTime := et + fr.bestShift
			msg += fmt.Sprintf("  Edge %d: %s\n", i+1, formatSecondsAsTimestamp(edgeAbsTime))
		}
		if len(fr.edgeTimes) == 2 {
			duration := math.Abs((fr.edgeTimes[1] + fr.bestShift) - (fr.edgeTimes[0] + fr.bestShift))
			msg += fmt.Sprintf("\nEvent duration: %.4f sec\n", duration)
		}

		// Count samples between event edges and find the minimum theoretical value
		if len(fr.edgeTimes) == 2 && len(fr.sampledTimes) > 0 {
			edge1Abs := fr.edgeTimes[0] + fr.bestShift
			edge2Abs := fr.edgeTimes[1] + fr.bestShift
			if edge1Abs > edge2Abs {
				edge1Abs, edge2Abs = edge2Abs, edge1Abs
			}
			sampleCount := 0
			minTheoretical := math.MaxFloat64
			for i, t := range fr.sampledTimes {
				if t >= edge1Abs && t <= edge2Abs {
					sampleCount++
					if fr.sampledVals[i] < minTheoretical {
						minTheoretical = fr.sampledVals[i]
					}
				}
			}
			msg += fmt.Sprintf("\nSamples between event edges: %d\n", sampleCount)
			if sampleCount > 0 {
				msg += fmt.Sprintf("Minimum theoretical value at event: %.4f\n", minTheoretical)
			}
		}

		fmt.Print(msg)

		// Log fit results
		logAction(fmt.Sprintf("Fit result: NCC=%.4f, time offset=%.4f sec, path offset=%.3f km", fr.bestNCC, fr.bestShift, params.PathPerpendicularOffsetKm))
		for i, et := range fr.edgeTimes {
			logAction(fmt.Sprintf("  Edge %d: %s", i+1, formatSecondsAsTimestamp(et+fr.bestShift)))
		}
		if len(fr.edgeTimes) == 2 {
			duration := math.Abs((fr.edgeTimes[1] + fr.bestShift) - (fr.edgeTimes[0] + fr.bestShift))
			logAction(fmt.Sprintf("  Event duration: %.4f sec", duration))
		}
		if len(fr.edgeTimes) == 2 && len(fr.sampledTimes) > 0 {
			edge1Abs := fr.edgeTimes[0] + fr.bestShift
			edge2Abs := fr.edgeTimes[1] + fr.bestShift
			if edge1Abs > edge2Abs {
				edge1Abs, edge2Abs = edge2Abs, edge1Abs
			}
			sampleCount := 0
			minTheoretical := math.MaxFloat64
			for i, t := range fr.sampledTimes {
				if t >= edge1Abs && t <= edge2Abs {
					sampleCount++
					if fr.sampledVals[i] < minTheoretical {
						minTheoretical = fr.sampledVals[i]
					}
				}
			}
			logAction(fmt.Sprintf("  Samples between event edges: %d", sampleCount))
			if sampleCount > 0 {
				logAction(fmt.Sprintf("  Minimum theoretical value at event: %.4f", minTheoretical))
			}
		}

		edgeLabel := widget.NewLabel(msg)
		edgeLabel.Wrapping = fyne.TextWrapWord
		spacer := canvas.NewRectangle(color.Transparent)
		spacer.SetMinSize(fyne.NewSize(750, 0))
		edgeContainer := container.NewVBox(spacer, edgeLabel)
		dialog.ShowCustom("Fit Edge Times", "OK", edgeContainer, w)
	}

	// Write the occultation parameters to the results folder
	if resultsFolder != "" {
		data, err := json5.Marshal(params)
		if err != nil {
			fmt.Printf("Warning: could not marshal parameters: %v\n", err)
		} else {
			var indented []byte
			if err := json5.Indent(&indented, data, "", "  "); err != nil {
				fmt.Printf("Warning: could not format parameters: %v\n", err)
			} else {
				savePath := filepath.Join(resultsFolder, "occultation_parameters.occparams")
				if err := os.WriteFile(savePath, indented, 0644); err != nil {
					fmt.Printf("Warning: could not write parameters to results folder: %v\n", err)
				} else {
					logAction(fmt.Sprintf("Occultation parameters written to: %s", savePath))
				}
			}
		}
	}

	return nil
}

// runMonteCarloRefit takes the sampled theoretical curve from the best fit,
// adds noise by sampling with replacement from extractedNoise, and re-runs
// the fit to get new edge times.
// mcRefitResult holds the Monte Carlo refit output along with the noisy observation used.
type mcRefitResult struct {
	fr          *fitResult
	noisyValues []float64
	pathOffset  float64
}

func runMonteCarloRefit(candidates []*precomputedCurve, fr *fitResult, noiseSigma float64) (*mcRefitResult, error) {
	if len(fr.sampledTimes) == 0 || len(fr.sampledVals) == 0 {
		return nil, fmt.Errorf("no sampled theoretical curve available for Monte Carlo")
	}
	if noiseSigma == 0 {
		return nil, fmt.Errorf("no noise sigma available — run Normalize Baseline first")
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no precomputed candidate curves available")
	}

	// Create a noisy version of the sampled theoretical curve by adding
	// Gaussian noise with the measured baseline noise sigma.
	noisyValues := make([]float64, len(fr.sampledVals))
	for i, v := range fr.sampledVals {
		noisyValues[i] = v + rand.NormFloat64()*noiseSigma
	}

	// Search across precomputed path offset candidates
	var bestFr *fitResult
	bestNCC := -1.0
	bestPathOffset := candidates[0].pathOffset
	for _, pc := range candidates {
		mcFr, err := nccSlidingFit(pc, fr.sampledTimes, noisyValues)
		if err != nil {
			continue
		}
		if mcFr.bestNCC > bestNCC {
			bestNCC = mcFr.bestNCC
			bestFr = mcFr
			bestPathOffset = pc.pathOffset
		}
	}
	if bestFr == nil {
		return nil, fmt.Errorf("all path offset candidates failed in Monte Carlo refit")
	}
	return &mcRefitResult{fr: bestFr, noisyValues: noisyValues, pathOffset: bestPathOffset}, nil
}

// performFit runs the NCC sliding fit between the theoretical diffraction light curve
// and the observed target curve, then displays the results in popup windows.
func performFit(app fyne.App, w fyne.Window, params *OccultationParameters, targetTimes, targetValues []float64, showDiagnostics bool) (*fitResult, *precomputedCurve, error) {
	pc, err := buildPrecomputedCurve(params)
	if err != nil {
		return nil, nil, err
	}
	fr, err := nccSlidingFit(pc, targetTimes, targetValues)
	if err != nil {
		return nil, nil, err
	}
	return fr, pc, displayFitResult(app, w, params, fr, targetTimes, targetValues, showDiagnostics)
}

// mcTrialsResult holds the accumulated edge time statistics from Monte Carlo trials.
type mcTrialsResult struct {
	numTrials   int
	numEdges    int
	edgeMeans   []float64
	edgeStds    []float64
	edgeAll     [][]float64 // edgeAll[edgeIdx][trial] — individual edge times
	pathOffsets []float64   // path offset for each trial
}

// runMonteCarloTrials runs numTrials Monte Carlo re-noise refits and computes
// the mean and standard deviation of the edge times. Safe to call from a goroutine.
// Candidates are the precomputed theoretical light curves from the fit search.
func runMonteCarloTrials(candidates []*precomputedCurve, fr *fitResult, noiseSigma float64, numTrials int, abort *atomic.Bool, onProgress func(float64)) (*mcTrialsResult, error) {
	if len(fr.sampledTimes) == 0 || len(fr.sampledVals) == 0 {
		return nil, fmt.Errorf("no sampled theoretical curve available — run a fit first")
	}
	if noiseSigma == 0 {
		return nil, fmt.Errorf("no noise sigma available — run Normalize Baseline first")
	}
	if len(fr.edgeTimes) == 0 {
		return nil, fmt.Errorf("no edge times in fit result")
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no candidate curves available — run a fit first")
	}
	fmt.Printf("Using %d candidate curves for Monte Carlo path offset search\n", len(candidates))

	numEdges := len(fr.edgeTimes)
	// edgeAccum[edgeIdx][trial] = absolute edge time for that trial
	edgeAccum := make([][]float64, numEdges)
	for i := range edgeAccum {
		edgeAccum[i] = make([]float64, 0, numTrials)
	}
	pathOffsets := make([]float64, 0, numTrials)

	for trial := 0; trial < numTrials; trial++ {
		if abort != nil && abort.Load() {
			fmt.Printf("Monte Carlo aborted after %d trials\n", trial)
			break
		}
		mcResult, err := runMonteCarloRefit(candidates, fr, noiseSigma)
		if err != nil {
			fmt.Printf("Monte Carlo trial %d failed: %v\n", trial+1, err)
			continue
		}
		mcFr := mcResult.fr
		for i, et := range mcFr.edgeTimes {
			if i < numEdges {
				edgeAccum[i] = append(edgeAccum[i], et+mcFr.bestShift)
			}
		}
		pathOffsets = append(pathOffsets, mcResult.pathOffset)
		if onProgress != nil {
			onProgress(float64(trial+1) / float64(numTrials))
		}
	}

	// Compute mean and std for each edge
	edgeMeans := make([]float64, numEdges)
	edgeStds := make([]float64, numEdges)
	for i := 0; i < numEdges; i++ {
		n := len(edgeAccum[i])
		if n == 0 {
			continue
		}
		var sum float64
		for _, v := range edgeAccum[i] {
			sum += v
		}
		mean := sum / float64(n)
		edgeMeans[i] = mean

		if n > 1 {
			var sumSq float64
			for _, v := range edgeAccum[i] {
				d := v - mean
				sumSq += d * d
			}
			edgeStds[i] = math.Sqrt(sumSq / float64(n-1))
		}
	}

	completedTrials := len(pathOffsets)
	if completedTrials == 0 {
		return nil, fmt.Errorf("no Monte Carlo trials completed successfully")
	}

	return &mcTrialsResult{
		numTrials:   completedTrials,
		numEdges:    numEdges,
		edgeMeans:   edgeMeans,
		edgeStds:    edgeStds,
		edgeAll:     edgeAccum,
		pathOffsets: pathOffsets,
	}, nil
}

// runNIETrials runs numTrials Noise-In-Event trials. Each trial generates a noisy
// baseline of nPoints values drawn from N(1.0, noiseSigma), slides a window of
// windowWidth across it, and records the minimum window mean (the biggest dip).
// Returns the slice of minimum window means.
func runNIETrials(numTrials, nPoints, windowWidth int, noiseSigma float64, abort *atomic.Bool, onProgress func(float64)) ([]float64, error) {
	if windowWidth < 1 {
		return nil, fmt.Errorf("event window width must be at least 1")
	}
	if nPoints < windowWidth {
		return nil, fmt.Errorf("light curve length (%d) is shorter than event window width (%d)", nPoints, windowWidth)
	}

	// Throttle progress callbacks to at most 100 updates so the event queue
	// is not flooded (NIE trials are fast and could generate thousands of
	// fyne.Do calls before the abort button click is ever processed).
	progressStep := numTrials / 100
	if progressStep < 1 {
		progressStep = 1
	}

	minMeans := make([]float64, 0, numTrials)
	for trial := 0; trial < numTrials; trial++ {
		if abort != nil && abort.Load() {
			fmt.Printf("NIE analysis aborted after %d trials\n", trial)
			break
		}

		// Generate noisy baseline: N(1.0, noiseSigma)
		seq := make([]float64, nPoints)
		for i := range seq {
			seq[i] = 1.0 + rand.NormFloat64()*noiseSigma
		}

		// Sliding window: find minimum mean (the biggest dip)
		var windowSum float64
		for i := 0; i < windowWidth; i++ {
			windowSum += seq[i]
		}
		minMean := windowSum / float64(windowWidth)
		for i := windowWidth; i < nPoints; i++ {
			windowSum += seq[i] - seq[i-windowWidth]
			m := windowSum / float64(windowWidth)
			if m < minMean {
				minMean = m
			}
		}
		minMeans = append(minMeans, minMean)

		if onProgress != nil && (trial+1)%progressStep == 0 {
			onProgress(float64(trial+1) / float64(numTrials))
		}
	}
	if onProgress != nil {
		onProgress(1.0)
	}

	if len(minMeans) == 0 {
		return nil, fmt.Errorf("no NIE trials completed")
	}
	return minMeans, nil
}

// createNIEHistogramImage renders a histogram of NIE minimum window means with a
// Gaussian fit overlay and a blue vertical line at eventDrop. Returns the image, mean, and sigma.
func createNIEHistogramImage(minMeans []float64, windowWidth int, eventDrop float64, occultationTitle string, plotWidth, plotHeight int) (image.Image, float64, float64, error) {
	n := float64(len(minMeans))
	var sum float64
	for _, v := range minMeans {
		sum += v
	}
	mean := sum / n

	var sumSq float64
	for _, v := range minMeans {
		d := v - mean
		sumSq += d * d
	}
	sigma := math.Sqrt(sumSq / n)

	plt := plot.New()
	if grayPlotBackground {
		plt.BackgroundColor = plotBackgroundGray
	}

	plt.Title.TextStyle.Font.Typeface = "Liberation"
	plt.Title.TextStyle.Font.Variant = "Sans"
	plt.Title.TextStyle.Font.Size = vg.Points(14)
	plt.Title.TextStyle.Font.Weight = 2

	plt.X.Label.TextStyle.Font.Typeface = "Liberation"
	plt.X.Label.TextStyle.Font.Variant = "Sans"
	plt.X.Label.TextStyle.Font.Size = vg.Points(12)
	plt.X.Label.TextStyle.Font.Weight = 2

	plt.Y.Label.TextStyle.Font.Typeface = "Liberation"
	plt.Y.Label.TextStyle.Font.Variant = "Sans"
	plt.Y.Label.TextStyle.Font.Size = vg.Points(12)
	plt.Y.Label.TextStyle.Font.Weight = 2

	plt.X.Tick.Label.Font.Typeface = "Liberation"
	plt.X.Tick.Label.Font.Variant = "Sans"
	plt.X.Tick.Label.Font.Size = vg.Points(10)

	plt.Y.Tick.Label.Font.Typeface = "Liberation"
	plt.Y.Tick.Label.Font.Variant = "Sans"
	plt.Y.Tick.Label.Font.Size = vg.Points(10)

	if occultationTitle != "" {
		plt.Title.Text = fmt.Sprintf("%s — NIE Analysis (%d trials)", occultationTitle, len(minMeans))
	} else {
		plt.Title.Text = fmt.Sprintf("NIE Analysis (%d trials)", len(minMeans))
	}
	plt.X.Label.Text = "Drop position (drops are bigger toward the right)"
	plt.Y.Label.Text = "Density"

	plt.Add(plotter.NewGrid())

	vals := make(plotter.Values, len(minMeans))
	copy(vals, minMeans)

	numBins := int(math.Sqrt(n))
	if numBins < 10 {
		numBins = 10
	}

	hist, err := plotter.NewHist(vals, numBins)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to create NIE histogram: %w", err)
	}
	hist.Normalize(1)
	hist.FillColor = color.RGBA{R: 100, G: 200, B: 120, A: 180}
	hist.Color = color.RGBA{R: 0, G: 130, B: 30, A: 255}
	plt.Add(hist)
	plt.Legend.Add(fmt.Sprintf("Noise induced drops at event size %d", windowWidth), hist)

	// Compute y-axis max and half-peak from Gaussian fit (used for line heights only, not plotted)
	var yMax, halfPeak float64
	if sigma > 0 {
		gaussPeak := 1.0 / (sigma * math.Sqrt(2*math.Pi))
		yMax = gaussPeak * 1.5
		halfPeak = gaussPeak * 0.5
	} else {
		yMax = 10.0
	}

	// Black vertical line at x=1.0 (baseline level) — half the Gaussian peak height
	baseLinePts := plotter.XYs{{X: 1.0, Y: 0}, {X: 1.0, Y: halfPeak}}
	baseLine, err := plotter.NewLine(baseLinePts)
	if err == nil {
		baseLine.Color = color.RGBA{R: 0, G: 0, B: 0, A: 255}
		baseLine.Width = vg.Points(2)
		plt.Add(baseLine)
		plt.Legend.Add("Baseline", baseLine)
	}

	// Blue vertical line at the event drop (square wave approximation) — half the Gaussian peak height
	vLinePts := plotter.XYs{{X: eventDrop, Y: 0}, {X: eventDrop, Y: halfPeak}}
	vLine, err2 := plotter.NewLine(vLinePts)
	if err2 == nil {
		vLine.Color = color.RGBA{R: 0, G: 0, B: 220, A: 255}
		vLine.Width = vg.Points(2)
		vLine.Dashes = []vg.Length{vg.Points(6), vg.Points(3)}
		plt.Add(vLine)
		plt.Legend.Add(fmt.Sprintf("Event bottom @ %.4f", eventDrop), vLine)
	}

	// Gray vertical line at x=0.0 (zero level) — half the height of the event line
	zeroLinePts := plotter.XYs{{X: 0.0, Y: 0}, {X: 0.0, Y: halfPeak / 2}}
	zeroLine, err3 := plotter.NewLine(zeroLinePts)
	if err3 == nil {
		zeroLine.Color = color.RGBA{R: 220, G: 180, B: 0, A: 255}
		zeroLine.Width = vg.Points(2)
		zeroLine.Dashes = []vg.Length{vg.Points(6), vg.Points(3)}
		plt.Add(zeroLine)
		plt.Legend.Add("zero level", zeroLine)
	}

	// Reversed x-axis: 1.2 on the left, -0.2 (or eventDrop with margin) on the right
	xMin := -0.2
	if eventDrop < xMin {
		xMin = eventDrop - 0.1
	}
	plt.X.Scale = reverseNorm{}
	plt.X.Min = xMin
	plt.X.Max = 1.2
	plt.Y.Min = 0.0
	plt.Y.Max = yMax

	// Center the legend horizontally
	plt.Legend.Top = true
	plt.Legend.Left = false
	plt.Legend.XOffs = vg.Points(-120)

	width := vg.Length(plotWidth) * vg.Inch / 96
	height := vg.Length(plotHeight) * vg.Inch / 96

	img := vgimg.New(width, height)
	dc := draw.New(img)
	plt.Draw(dc)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img.Image()); err != nil {
		return nil, 0, 0, fmt.Errorf("failed to encode NIE histogram PNG: %w", err)
	}

	nieHistPath := filepath.Join(appDir, "nieHistogram.png")
	if resultsFolder != "" {
		nieHistPath = filepath.Join(resultsFolder, "nieHistogram.png")
	}
	if err := os.WriteFile(nieHistPath, buf.Bytes(), 0644); err != nil {
		fmt.Printf("Warning: could not save nieHistogram.png: %v\n", err)
	}

	histImg, err := png.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to decode NIE histogram PNG: %w", err)
	}
	return histImg, mean, sigma, nil
}

// fitSearchResult holds the output of runFitSearch for display.
type fitSearchResult struct {
	results        []searchResult
	bestIdx        int
	bestPathOffset float64
}

// runFitSearch computes the NCC fit for a range of path perpendicular offsets.
// It is safe to call from a goroutine. UI display is handled separately.
func runFitSearch(params *OccultationParameters, targetTimes, targetValues []float64, initialOffset, finalOffset float64, numSteps int, abort *atomic.Bool, onProgress func(float64)) (*fitSearchResult, error) {
	results := make([]searchResult, 0, numSteps)

	for step := 0; step < numSteps; step++ {
		if abort != nil && abort.Load() {
			fmt.Printf("Fit search aborted after %d of %d steps\n", step, numSteps)
			break
		}

		var offset float64
		if numSteps == 1 {
			offset = initialOffset
		} else {
			offset = initialOffset + float64(step)*(finalOffset-initialOffset)/float64(numSteps-1)
		}

		// Set the path offset for this iteration
		params.PathPerpendicularOffsetKm = offset

		pc, err := buildPrecomputedCurve(params)
		if err != nil {
			fmt.Printf("Fit at path offset %.3f km failed: %v\n", offset, err)
			continue
		}
		fr, err := nccSlidingFit(pc, targetTimes, targetValues)
		if err != nil {
			fmt.Printf("Fit at path offset %.3f km failed: %v\n", offset, err)
			continue
		}

		fmt.Printf("Path offset %.3f km: peak NCC=%.6f at time shift=%.4f sec\n", offset, fr.bestNCC, fr.bestShift)
		results = append(results, searchResult{pathOffset: offset, peakNCC: fr.bestNCC, fr: fr, pc: pc})

		if onProgress != nil {
			onProgress(float64(step+1) / float64(numSteps))
		}
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("all path offset fits failed")
	}

	// Find the best path offset
	bestIdx := 0
	for i, r := range results {
		if r.peakNCC > results[bestIdx].peakNCC {
			bestIdx = i
		}
	}
	bestPathOffset := results[bestIdx].pathOffset
	fmt.Printf("Best path offset: %.3f km (peak NCC=%.6f)\n", bestPathOffset, results[bestIdx].peakNCC)

	return &fitSearchResult{results: results, bestIdx: bestIdx, bestPathOffset: bestPathOffset}, nil
}

// displayFitSearchResult shows the path offset plot and the full fit for the best offset.
// Must be called on the main thread.
func displayFitSearchResult(app fyne.App, w fyne.Window, params *OccultationParameters, fsr *fitSearchResult, targetTimes, targetValues []float64, showDiagnostics bool) (*fitResult, error) {
	if showDiagnostics {
		searchPlotImg, err := createPathOffsetPlotImage(fsr.results, params.Title, 1000, 500)
		if err != nil {
			return nil, fmt.Errorf("failed to create path offset plot: %w", err)
		}

		searchWindow := app.NewWindow("Peak NCC vs Path Offset")
		searchCanvas := canvas.NewImageFromImage(searchPlotImg)
		searchCanvas.FillMode = canvas.ImageFillOriginal
		searchWindow.SetContent(container.NewScroll(searchCanvas))
		searchWindow.Resize(fyne.NewSize(1050, 550))
		searchWindow.CenterOnScreen()
		searchWindow.Show()
	}

	params.PathPerpendicularOffsetKm = fsr.bestPathOffset
	bestFr := fsr.results[fsr.bestIdx].fr
	err := displayFitResult(app, w, params, bestFr, targetTimes, targetValues, showDiagnostics)
	return bestFr, err
}

// searchResult holds results for one path offset in a search.
type searchResult struct {
	pathOffset float64
	peakNCC    float64
	fr         *fitResult
	pc         *precomputedCurve
}

// createPathOffsetPlotImage renders peak NCC versus path offset.
func createPathOffsetPlotImage(results []searchResult, occultationTitle string, plotWidth, plotHeight int) (image.Image, error) {
	plt := plot.New()
	if grayPlotBackground {
		plt.BackgroundColor = plotBackgroundGray
	}

	plt.Title.TextStyle.Font.Typeface = "Liberation"
	plt.Title.TextStyle.Font.Variant = "Sans"
	plt.Title.TextStyle.Font.Size = vg.Points(14)
	plt.Title.TextStyle.Font.Weight = 2

	plt.X.Label.TextStyle.Font.Typeface = "Liberation"
	plt.X.Label.TextStyle.Font.Variant = "Sans"
	plt.X.Label.TextStyle.Font.Size = vg.Points(12)
	plt.X.Label.TextStyle.Font.Weight = 2

	plt.Y.Label.TextStyle.Font.Typeface = "Liberation"
	plt.Y.Label.TextStyle.Font.Variant = "Sans"
	plt.Y.Label.TextStyle.Font.Size = vg.Points(12)
	plt.Y.Label.TextStyle.Font.Weight = 2

	plt.X.Tick.Label.Font.Typeface = "Liberation"
	plt.X.Tick.Label.Font.Variant = "Sans"
	plt.X.Tick.Label.Font.Size = vg.Points(10)

	plt.Y.Tick.Label.Font.Typeface = "Liberation"
	plt.Y.Tick.Label.Font.Variant = "Sans"
	plt.Y.Tick.Label.Font.Size = vg.Points(10)

	plt.Title.Text = "Peak NCC vs Path Perpendicular Offset"
	if occultationTitle != "" {
		plt.Title.Text = occultationTitle + " — " + plt.Title.Text
	}
	plt.X.Label.Text = "Path offset (km)"
	plt.Y.Label.Text = "Peak NCC"

	xRange := math.Abs(results[len(results)-1].pathOffset - results[0].pathOffset)
	if xRange > 0 {
		xStep := xRange / 20
		plt.X.Tick.Marker = lightcurve.StepTicks{Step: xStep, Format: "%.2f"}
	}
	plt.Y.Tick.Marker = lightcurve.StepTicks{Step: 0.05, Format: "%.2f"}
	plt.Add(plotter.NewGrid())

	// Dashed black line at y=0
	zeroLinePts := make(plotter.XYs, 2)
	zeroLinePts[0].X = results[0].pathOffset
	zeroLinePts[0].Y = 0
	zeroLinePts[1].X = results[len(results)-1].pathOffset
	zeroLinePts[1].Y = 0
	zeroLine, err := plotter.NewLine(zeroLinePts)
	if err == nil {
		zeroLine.Color = color.Black
		zeroLine.Width = vg.Points(1)
		zeroLine.Dashes = []vg.Length{vg.Points(6), vg.Points(4)}
		plt.Add(zeroLine)
	}

	pts := make(plotter.XYs, len(results))
	bestIdx := 0
	for i, r := range results {
		pts[i].X = r.pathOffset
		pts[i].Y = r.peakNCC
		if r.peakNCC > results[bestIdx].peakNCC {
			bestIdx = i
		}
	}

	line, err := plotter.NewLine(pts)
	if err != nil {
		return nil, fmt.Errorf("failed to create line plot: %w", err)
	}
	line.Color = color.RGBA{R: 0, G: 0, B: 200, A: 255}
	line.Width = vg.Points(1.5)
	plt.Add(line)

	scatter, err := plotter.NewScatter(pts)
	if err == nil {
		scatter.Color = color.RGBA{R: 0, G: 0, B: 200, A: 255}
		scatter.GlyphStyle.Shape = draw.CircleGlyph{}
		scatter.GlyphStyle.Radius = vg.Points(3)
		plt.Add(scatter)
	}

	// Highlight the best with a red point
	peakPt := make(plotter.XYs, 1)
	peakPt[0].X = results[bestIdx].pathOffset
	peakPt[0].Y = results[bestIdx].peakNCC
	peakScatter, err := plotter.NewScatter(peakPt)
	if err == nil {
		peakScatter.Color = color.RGBA{R: 255, G: 0, B: 0, A: 255}
		peakScatter.GlyphStyle.Shape = draw.CircleGlyph{}
		peakScatter.GlyphStyle.Radius = vg.Points(6)
		plt.Add(peakScatter)
	}

	plt.Legend.Add(fmt.Sprintf("Best: %.3f km, NCC=%.4f", results[bestIdx].pathOffset, results[bestIdx].peakNCC), line)
	plt.Legend.Top = true
	plt.Legend.Left = false

	plt.Y.Max = 1.1

	width := vg.Length(plotWidth) * vg.Inch / 96
	height := vg.Length(plotHeight) * vg.Inch / 96

	img := vgimg.New(width, height)
	dc := draw.New(img)
	plt.Draw(dc)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img.Image()); err != nil {
		return nil, fmt.Errorf("failed to encode plot PNG: %w", err)
	}

	// Save to the results folder
	peakNCCPath := filepath.Join(appDir, "peakNCCvsPathOffset.png")
	if resultsFolder != "" {
		peakNCCPath = filepath.Join(resultsFolder, "peakNCCvsPathOffset.png")
	}
	if err := os.WriteFile(peakNCCPath, buf.Bytes(), 0644); err != nil {
		fmt.Printf("Warning: could not save peakNCCvsPathOffset.png: %v\n", err)
	}

	goImg, err := png.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		return nil, fmt.Errorf("failed to decode plot PNG: %w", err)
	}

	return goImg, nil
}

// medianTimeDelta returns the median of consecutive time differences.
func medianTimeDelta(times []float64) float64 {
	if len(times) < 2 {
		return 0
	}
	deltas := make([]float64, len(times)-1)
	for i := 1; i < len(times); i++ {
		deltas[i-1] = times[i] - times[i-1]
	}
	sort.Float64s(deltas)
	mid := len(deltas) / 2
	if len(deltas)%2 == 0 {
		return (deltas[mid-1] + deltas[mid]) / 2
	}
	return deltas[mid]
}

// createNCCPlotImage renders the NCC results as a gonum/plot image.
func createNCCPlotImage(results []nccResult, occultationTitle string, plotWidth, plotHeight int) (image.Image, error) {
	plt := plot.New()
	if grayPlotBackground {
		plt.BackgroundColor = plotBackgroundGray
	}

	// Font styling (same pattern as plot_widget.go)
	plt.Title.TextStyle.Font.Typeface = "Liberation"
	plt.Title.TextStyle.Font.Variant = "Sans"
	plt.Title.TextStyle.Font.Size = vg.Points(14)
	plt.Title.TextStyle.Font.Weight = 2

	plt.X.Label.TextStyle.Font.Typeface = "Liberation"
	plt.X.Label.TextStyle.Font.Variant = "Sans"
	plt.X.Label.TextStyle.Font.Size = vg.Points(12)
	plt.X.Label.TextStyle.Font.Weight = 2

	plt.Y.Label.TextStyle.Font.Typeface = "Liberation"
	plt.Y.Label.TextStyle.Font.Variant = "Sans"
	plt.Y.Label.TextStyle.Font.Size = vg.Points(12)
	plt.Y.Label.TextStyle.Font.Weight = 2

	plt.X.Tick.Label.Font.Typeface = "Liberation"
	plt.X.Tick.Label.Font.Variant = "Sans"
	plt.X.Tick.Label.Font.Size = vg.Points(10)

	plt.Y.Tick.Label.Font.Typeface = "Liberation"
	plt.Y.Tick.Label.Font.Variant = "Sans"
	plt.Y.Tick.Label.Font.Size = vg.Points(10)

	plt.Title.Text = "Normalized Cross-Correlation vs Time Offset"
	if occultationTitle != "" {
		plt.Title.Text = occultationTitle + " — " + plt.Title.Text
	}
	plt.X.Label.Text = "Time offset (seconds)"
	plt.Y.Label.Text = "NCC"

	// Add grid
	plt.Add(plotter.NewGrid())

	// Dashed black line at y=0
	zeroLinePts := make(plotter.XYs, 2)
	zeroLinePts[0].X = results[0].offset
	zeroLinePts[0].Y = 0
	zeroLinePts[1].X = results[len(results)-1].offset
	zeroLinePts[1].Y = 0
	zeroLine, err := plotter.NewLine(zeroLinePts)
	if err == nil {
		zeroLine.Color = color.Black
		zeroLine.Width = vg.Points(1)
		zeroLine.Dashes = []vg.Length{vg.Points(6), vg.Points(4)}
		plt.Add(zeroLine)
	}

	// Build XY data for unweighted NCC line
	unweightedPts := make(plotter.XYs, len(results))
	for i, r := range results {
		unweightedPts[i].X = r.offset
		unweightedPts[i].Y = r.ncc
	}

	unweightedLine, err := plotter.NewLine(unweightedPts)
	if err != nil {
		return nil, fmt.Errorf("failed to create unweighted line plot: %w", err)
	}
	unweightedLine.Color = color.RGBA{R: 150, G: 150, B: 150, A: 255}
	unweightedLine.Width = vg.Points(1)
	plt.Add(unweightedLine)

	// Build XY data for weighted NCC line
	weightedPts := make(plotter.XYs, len(results))
	bestIdx := 0
	for i, r := range results {
		weightedPts[i].X = r.offset
		weightedPts[i].Y = r.weightedNCC
		if r.weightedNCC > results[bestIdx].weightedNCC {
			bestIdx = i
		}
	}

	weightedLine, err := plotter.NewLine(weightedPts)
	if err != nil {
		return nil, fmt.Errorf("failed to create weighted line plot: %w", err)
	}
	weightedLine.Color = color.RGBA{R: 0, G: 0, B: 200, A: 255}
	weightedLine.Width = vg.Points(1.5)
	plt.Add(weightedLine)

	// Highlight the peak of weighted NCC with a red scatter point
	peakPt := make(plotter.XYs, 1)
	peakPt[0].X = results[bestIdx].offset
	peakPt[0].Y = results[bestIdx].weightedNCC
	peakScatter, err := plotter.NewScatter(peakPt)
	if err == nil {
		peakScatter.Color = color.RGBA{R: 255, G: 0, B: 0, A: 255}
		peakScatter.GlyphStyle.Shape = draw.CircleGlyph{}
		peakScatter.GlyphStyle.Radius = vg.Points(5)
		plt.Add(peakScatter)
	}

	// Add legend entries
	plt.Legend.Add("NCC (unweighted)", unweightedLine)
	plt.Legend.Add(fmt.Sprintf("NCC*w peak: offset=%.3fs, val=%.4f", results[bestIdx].offset, results[bestIdx].weightedNCC), weightedLine)
	plt.Legend.Top = true
	plt.Legend.Left = false

	// Fixed Y maximum
	plt.Y.Max = 1.1

	// Render to image
	width := vg.Length(plotWidth) * vg.Inch / 96
	height := vg.Length(plotHeight) * vg.Inch / 96

	img := vgimg.New(width, height)
	dc := draw.New(img)
	plt.Draw(dc)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img.Image()); err != nil {
		return nil, fmt.Errorf("failed to encode plot PNG: %w", err)
	}
	goImg, err := png.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		return nil, fmt.Errorf("failed to decode plot PNG: %w", err)
	}

	return goImg, nil
}

// createOverlayPlotImage renders the target light curve and the theoretical curve
// (shifted by bestOffset) together in a single plot, with geometric shadow edges
// shown as vertical dashed lines.
func createOverlayPlotImage(curve []timeIntensityPoint, bestOffset float64, edgeTimes []float64, targetTimes, targetValues, sampledTimes, sampledVals []float64, bestNCC float64, occultationTitle string, plotWidth, plotHeight int, edgeStds []float64) (image.Image, error) {
	plt := plot.New()
	if grayPlotBackground {
		plt.BackgroundColor = plotBackgroundGray
	}

	// Font styling
	plt.Title.TextStyle.Font.Typeface = "Liberation"
	plt.Title.TextStyle.Font.Variant = "Sans"
	plt.Title.TextStyle.Font.Size = vg.Points(14)
	plt.Title.TextStyle.Font.Weight = 2

	plt.X.Label.TextStyle.Font.Typeface = "Liberation"
	plt.X.Label.TextStyle.Font.Variant = "Sans"
	plt.X.Label.TextStyle.Font.Size = vg.Points(12)
	plt.X.Label.TextStyle.Font.Weight = 2

	plt.Y.Label.TextStyle.Font.Typeface = "Liberation"
	plt.Y.Label.TextStyle.Font.Variant = "Sans"
	plt.Y.Label.TextStyle.Font.Size = vg.Points(12)
	plt.Y.Label.TextStyle.Font.Weight = 2

	plt.X.Tick.Label.Font.Typeface = "Liberation"
	plt.X.Tick.Label.Font.Variant = "Sans"
	plt.X.Tick.Label.Font.Size = vg.Points(10)

	plt.Y.Tick.Label.Font.Typeface = "Liberation"
	plt.Y.Tick.Label.Font.Variant = "Sans"
	plt.Y.Tick.Label.Font.Size = vg.Points(10)

	plt.Title.Text = fmt.Sprintf("Fit Result (NCC=%.4f, offset=%s)", bestNCC, formatSecondsAsTimestamp(bestOffset))
	if occultationTitle != "" {
		plt.Title.Text = occultationTitle + " — " + plt.Title.Text
	}
	plt.X.Label.Text = "Time (timestamp)"
	plt.Y.Label.Text = "Intensity"
	plt.X.Tick.Marker = timestampTicker{}

	plt.Add(plotter.NewGrid())

	// Dashed black line at y=0
	zeroLinePts := make(plotter.XYs, 2)
	zeroLinePts[0].X = targetTimes[0]
	zeroLinePts[0].Y = 0
	zeroLinePts[1].X = targetTimes[len(targetTimes)-1]
	zeroLinePts[1].Y = 0
	zeroLine, err := plotter.NewLine(zeroLinePts)
	if err == nil {
		zeroLine.Color = color.Black
		zeroLine.Width = vg.Points(1)
		zeroLine.Dashes = []vg.Length{vg.Points(6), vg.Points(4)}
		plt.Add(zeroLine)
	}

	// Target light curve (scatter + line, blue)
	targetPts := make(plotter.XYs, len(targetTimes))
	for i := range targetTimes {
		targetPts[i].X = targetTimes[i]
		targetPts[i].Y = targetValues[i]
	}

	targetLine, err := plotter.NewLine(targetPts)
	if err != nil {
		return nil, fmt.Errorf("failed to create target line: %w", err)
	}
	targetLine.Color = color.RGBA{R: 0, G: 100, B: 200, A: 255}
	targetLine.Width = vg.Points(1)
	plt.Add(targetLine)

	targetScatter, err := plotter.NewScatter(targetPts)
	if err == nil {
		targetScatter.Color = color.RGBA{R: 0, G: 100, B: 200, A: 255}
		targetScatter.GlyphStyle.Shape = draw.CircleGlyph{}
		targetScatter.GlyphStyle.Radius = vg.Points(2.5)
		plt.Add(targetScatter)
	}

	// Theoretical curve shifted by the bestOffset (line, red)
	theoryPts := make(plotter.XYs, len(curve))
	for i, pt := range curve {
		theoryPts[i].X = pt.time + bestOffset
		theoryPts[i].Y = pt.intensity
	}

	theoryLine, err := plotter.NewLine(theoryPts)
	if err != nil {
		return nil, fmt.Errorf("failed to create theoretical line: %w", err)
	}
	theoryLine.Color = color.RGBA{R: 255, G: 170, B: 170, A: 255}
	theoryLine.Width = vg.Points(2)
	plt.Add(theoryLine)

	// Sampled theoretical points as red dots
	if len(sampledTimes) > 0 && len(sampledTimes) == len(sampledVals) {
		sampledPts := make(plotter.XYs, len(sampledTimes))
		for i := range sampledTimes {
			sampledPts[i].X = sampledTimes[i]
			sampledPts[i].Y = sampledVals[i]
		}
		sampledScatter, err := plotter.NewScatter(sampledPts)
		if err == nil {
			sampledScatter.Color = color.RGBA{R: 220, G: 30, B: 30, A: 255}
			sampledScatter.GlyphStyle.Shape = draw.CircleGlyph{}
			sampledScatter.GlyphStyle.Radius = vg.Points(3)
			plt.Add(sampledScatter)
		}
	}

	// Geometric shadow edges as vertical dashed lines (green)
	// Determine Y range from the target data for the line extent
	minY, maxY := targetValues[0], targetValues[0]
	for _, v := range targetValues {
		if v < minY {
			minY = v
		}
		if v > maxY {
			maxY = v
		}
	}
	for _, pt := range curve {
		if pt.intensity < minY {
			minY = pt.intensity
		}
		if pt.intensity > maxY {
			maxY = pt.intensity
		}
	}
	for _, et := range edgeTimes {
		edgeX := et + bestOffset // shift to the target time domain
		vlinePts := make(plotter.XYs, 2)
		vlinePts[0].X = edgeX
		vlinePts[0].Y = minY
		vlinePts[1].X = edgeX
		vlinePts[1].Y = maxY
		vline, err := plotter.NewLine(vlinePts)
		if err == nil {
			vline.Color = color.RGBA{R: 0, G: 180, B: 0, A: 255}
			vline.Width = vg.Points(1.5)
			vline.Dashes = []vg.Length{vg.Points(6), vg.Points(4)}
			plt.Add(vline)
		}
	}

	// Edge uncertainty lines: ±3σ dashed red vertical lines
	if len(edgeStds) > 0 {
		for i, et := range edgeTimes {
			if i >= len(edgeStds) {
				break
			}
			sigma3 := 3.0 * edgeStds[i]
			edgeX := et + bestOffset
			for _, delta := range []float64{-sigma3, sigma3} {
				x := edgeX + delta
				pts := make(plotter.XYs, 2)
				pts[0].X = x
				pts[0].Y = minY
				pts[1].X = x
				pts[1].Y = maxY
				vline, err := plotter.NewLine(pts)
				if err == nil {
					vline.Color = color.RGBA{R: 220, G: 30, B: 30, A: 255}
					vline.Width = vg.Points(1.5)
					vline.Dashes = []vg.Length{vg.Points(6), vg.Points(4)}
					plt.Add(vline)
				}
			}
		}
	}

	// Legend
	plt.Legend.Add("Observed", targetLine)
	plt.Legend.Add("Theoretical (fit)", theoryLine)
	plt.Legend.Add("Zero level", zeroLine)
	plt.Legend.Top = true
	plt.Legend.Left = true

	// Render to image
	width := vg.Length(plotWidth) * vg.Inch / 96
	height := vg.Length(plotHeight) * vg.Inch / 96

	img := vgimg.New(width, height)
	dc := draw.New(img)
	plt.Draw(dc)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img.Image()); err != nil {
		return nil, fmt.Errorf("failed to encode overlay PNG: %w", err)
	}

	// Save to fitPlot.png in the results folder (only for the initial fit, not Monte Carlo)
	if edgeStds == nil {
		fitPlotPath := filepath.Join(appDir, "fitPlot.png")
		if resultsFolder != "" {
			fitPlotPath = filepath.Join(resultsFolder, "fitPlot.png")
		}
		if err := os.WriteFile(fitPlotPath, buf.Bytes(), 0644); err != nil {
			fmt.Printf("Warning: could not save fitPlot.png: %v\n", err)
		}
	}

	goImg, err := png.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		return nil, fmt.Errorf("failed to decode overlay PNG: %w", err)
	}

	return goImg, nil
}

// createHistogramImage renders a histogram of values with a Gaussian curve fit overlay.
func createHistogramImage(values []float64, title, xLabel, occultationTitle string, plotWidth, plotHeight int) (image.Image, error) {
	n := float64(len(values))
	if n < 2 {
		return nil, fmt.Errorf("need at least 2 values for histogram")
	}

	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / n

	var sumSq float64
	for _, v := range values {
		d := v - mean
		sumSq += d * d
	}
	sigma := math.Sqrt(sumSq / (n - 1))

	plt := plot.New()
	if grayPlotBackground {
		plt.BackgroundColor = plotBackgroundGray
	}

	plt.Title.TextStyle.Font.Typeface = "Liberation"
	plt.Title.TextStyle.Font.Variant = "Sans"
	plt.Title.TextStyle.Font.Size = vg.Points(14)
	plt.Title.TextStyle.Font.Weight = 2

	plt.X.Label.TextStyle.Font.Typeface = "Liberation"
	plt.X.Label.TextStyle.Font.Variant = "Sans"
	plt.X.Label.TextStyle.Font.Size = vg.Points(12)
	plt.X.Label.TextStyle.Font.Weight = 2

	plt.Y.Label.TextStyle.Font.Typeface = "Liberation"
	plt.Y.Label.TextStyle.Font.Variant = "Sans"
	plt.Y.Label.TextStyle.Font.Size = vg.Points(12)
	plt.Y.Label.TextStyle.Font.Weight = 2

	plt.X.Tick.Label.Font.Typeface = "Liberation"
	plt.X.Tick.Label.Font.Variant = "Sans"
	plt.X.Tick.Label.Font.Size = vg.Points(10)

	plt.Y.Tick.Label.Font.Typeface = "Liberation"
	plt.Y.Tick.Label.Font.Variant = "Sans"
	plt.Y.Tick.Label.Font.Size = vg.Points(10)

	plt.Title.Text = fmt.Sprintf("%s (mean=%.4f, std=%.4f, n=%d)", title, mean, sigma, len(values))
	if occultationTitle != "" {
		plt.Title.Text = occultationTitle + " — " + plt.Title.Text
	}
	plt.X.Label.Text = xLabel
	plt.Y.Label.Text = "Density"

	plt.Add(plotter.NewGrid())

	vals := make(plotter.Values, len(values))
	copy(vals, values)

	numBins := int(math.Sqrt(n))
	if numBins < 10 {
		numBins = 10
	}

	hist, err := plotter.NewHist(vals, numBins)
	if err != nil {
		return nil, fmt.Errorf("failed to create histogram: %w", err)
	}
	hist.Normalize(1)
	hist.FillColor = color.RGBA{R: 100, G: 149, B: 237, A: 180}
	hist.Color = color.RGBA{R: 0, G: 0, B: 150, A: 255}
	plt.Add(hist)

	// Overlay Gaussian PDF curve
	if sigma > 0 {
		sorted := make([]float64, len(values))
		copy(sorted, values)
		sort.Float64s(sorted)
		xMin := sorted[0]
		xMax := sorted[len(sorted)-1]
		margin := (xMax - xMin) * 0.1
		xMin -= margin
		xMax += margin

		gaussPts := make(plotter.XYs, 200)
		for i := range gaussPts {
			x := xMin + float64(i)*(xMax-xMin)/199.0
			z := (x - mean) / sigma
			gaussPts[i].X = x
			gaussPts[i].Y = math.Exp(-0.5*z*z) / (sigma * math.Sqrt(2*math.Pi))
		}

		gaussLine, err := plotter.NewLine(gaussPts)
		if err == nil {
			gaussLine.Color = color.RGBA{R: 220, G: 30, B: 30, A: 255}
			gaussLine.Width = vg.Points(2)
			plt.Add(gaussLine)
			plt.Legend.Add(fmt.Sprintf("Gaussian (σ=%.4f)", sigma), gaussLine)
		}
	}

	plt.Legend.Top = true
	plt.Legend.Left = false

	width := vg.Length(plotWidth) * vg.Inch / 96
	height := vg.Length(plotHeight) * vg.Inch / 96

	img := vgimg.New(width, height)
	dc := draw.New(img)
	plt.Draw(dc)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img.Image()); err != nil {
		return nil, fmt.Errorf("failed to encode histogram PNG: %w", err)
	}
	histImg, err := png.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		return nil, fmt.Errorf("failed to decode histogram PNG: %w", err)
	}

	return histImg, nil
}

// createNoiseHistogramImage renders a histogram of noise values with a Gaussian fit overlay.
// Returns the image, mean, and sigma.
func createNoiseHistogramImage(noise []float64, occultationTitle string, plotWidth, plotHeight int) (image.Image, float64, float64, error) {
	n := float64(len(noise))
	var sum float64
	for _, v := range noise {
		sum += v
	}
	mean := sum / n

	var sumSq float64
	for _, v := range noise {
		d := v - mean
		sumSq += d * d
	}
	sigma := math.Sqrt(sumSq / n)

	plt := plot.New()
	if grayPlotBackground {
		plt.BackgroundColor = plotBackgroundGray
	}

	plt.Title.TextStyle.Font.Typeface = "Liberation"
	plt.Title.TextStyle.Font.Variant = "Sans"
	plt.Title.TextStyle.Font.Size = vg.Points(14)
	plt.Title.TextStyle.Font.Weight = 2

	plt.X.Label.TextStyle.Font.Typeface = "Liberation"
	plt.X.Label.TextStyle.Font.Variant = "Sans"
	plt.X.Label.TextStyle.Font.Size = vg.Points(12)
	plt.X.Label.TextStyle.Font.Weight = 2

	plt.Y.Label.TextStyle.Font.Typeface = "Liberation"
	plt.Y.Label.TextStyle.Font.Variant = "Sans"
	plt.Y.Label.TextStyle.Font.Size = vg.Points(12)
	plt.Y.Label.TextStyle.Font.Weight = 2

	plt.X.Tick.Label.Font.Typeface = "Liberation"
	plt.X.Tick.Label.Font.Variant = "Sans"
	plt.X.Tick.Label.Font.Size = vg.Points(10)

	plt.Y.Tick.Label.Font.Typeface = "Liberation"
	plt.Y.Tick.Label.Font.Variant = "Sans"
	plt.Y.Tick.Label.Font.Size = vg.Points(10)

	plt.Title.Text = fmt.Sprintf("Baseline Noise (mean=%.4f, sigma=%.4f, n=%d)", mean, sigma, len(noise))
	if occultationTitle != "" {
		plt.Title.Text = occultationTitle + " — " + plt.Title.Text
	}
	plt.X.Label.Text = "Noise (value - baseline)"
	plt.Y.Label.Text = "Density"

	plt.Add(plotter.NewGrid())

	vals := make(plotter.Values, len(noise))
	copy(vals, noise)

	numBins := int(math.Sqrt(n))
	if numBins < 10 {
		numBins = 10
	}

	hist, err := plotter.NewHist(vals, numBins)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to create histogram: %w", err)
	}
	hist.Normalize(1)
	hist.FillColor = color.RGBA{R: 100, G: 149, B: 237, A: 180}
	hist.Color = color.RGBA{R: 0, G: 0, B: 150, A: 255}
	plt.Add(hist)

	// Overlay Gaussian PDF curve
	if sigma > 0 {
		sorted := make([]float64, len(noise))
		copy(sorted, noise)
		sort.Float64s(sorted)
		xMin := sorted[0]
		xMax := sorted[len(sorted)-1]
		margin := (xMax - xMin) * 0.1
		xMin -= margin
		xMax += margin

		gaussPts := make(plotter.XYs, 200)
		for i := range gaussPts {
			x := xMin + float64(i)*(xMax-xMin)/199.0
			z := (x - mean) / sigma
			gaussPts[i].X = x
			gaussPts[i].Y = math.Exp(-0.5*z*z) / (sigma * math.Sqrt(2*math.Pi))
		}

		gaussLine, err := plotter.NewLine(gaussPts)
		if err == nil {
			gaussLine.Color = color.RGBA{R: 220, G: 30, B: 30, A: 255}
			gaussLine.Width = vg.Points(2)
			plt.Add(gaussLine)
			plt.Legend.Add(fmt.Sprintf("Gaussian (σ=%.4f)", sigma), gaussLine)
		}
	}

	plt.Legend.Top = true
	plt.Legend.Left = false

	width := vg.Length(plotWidth) * vg.Inch / 96
	height := vg.Length(plotHeight) * vg.Inch / 96

	img := vgimg.New(width, height)
	dc := draw.New(img)
	plt.Draw(dc)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img.Image()); err != nil {
		return nil, 0, 0, fmt.Errorf("failed to encode histogram PNG: %w", err)
	}

	// Save to baselineNoiseHistogram.png in the results folder
	noiseHistPath := filepath.Join(appDir, "baselineNoiseHistogram.png")
	if resultsFolder != "" {
		noiseHistPath = filepath.Join(resultsFolder, "baselineNoiseHistogram.png")
	}
	if err := os.WriteFile(noiseHistPath, buf.Bytes(), 0644); err != nil {
		fmt.Printf("Warning: could not save baselineNoiseHistogram.png: %v\n", err)
	}

	histImg, err := png.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to decode histogram PNG: %w", err)
	}

	return histImg, mean, sigma, nil
}
