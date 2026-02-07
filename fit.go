package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"sort"

	"GoPyOTE/lightcurve"

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
	curve     []timeIntensityPoint
	edgeTimes []float64
	nccCurve  []nccResult
	bestNCC   float64
	bestShift float64
}

// runSingleFit generates the theoretical curve for the given params and runs
// the NCC sliding fit against the target data. It returns the results without
// displaying anything.
func runSingleFit(params *OccultationParameters, targetTimes, targetValues []float64) (*fitResult, error) {
	lcData, edges, err := lightcurve.ExtractAndPlotLightCurve(
		nil,
		params.DXKmPerSec,
		params.DYKmPerSec,
		params.PathPerpendicularOffsetKm,
		params.FundamentalPlaneWidthKm,
		params.FundamentalPlaneWidthNumPoints,
		"occultImage16bit.png",
		"geometricShadow.png",
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

	theoreticalDuration := curve[len(curve)-1].time

	curveTimes := make([]float64, len(curve))
	for i, pt := range curve {
		curveTimes[i] = pt.time
	}

	framePeriod := medianTimeDelta(targetTimes)
	if framePeriod <= 0 {
		return nil, fmt.Errorf("could not determine frame period from target timestamps")
	}

	shiftStart := targetTimes[0] - theoreticalDuration
	shiftEnd := targetTimes[len(targetTimes)-1]

	numSteps := int((shiftEnd-shiftStart)/framePeriod) + 1
	results := make([]nccResult, 0, numSteps)
	sampled := make([]float64, len(targetTimes))

	for step := 0; step < numSteps; step++ {
		shift := shiftStart + float64(step)*framePeriod
		overlapCount := 0
		for i, t := range targetTimes {
			localT := t - shift
			if localT < 0 || localT > theoreticalDuration {
				sampled[i] = 1.0
			} else {
				sampled[i] = interpolateAt(curve, curveTimes, localT)
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

	// Compute weighted NCC: w = sqrt(overlapCount / maxOverlap), weightedNCC = ncc * w
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

	return &fitResult{
		curve:     curve,
		edgeTimes: edgeTimes,
		nccCurve:  results,
		bestNCC:   results[bestIdx].weightedNCC,
		bestShift: results[bestIdx].offset,
	}, nil
}

// displayFitResult shows the NCC plot, overlay plot, diffraction image, and edge times for a fit result.
func displayFitResult(app fyne.App, w fyne.Window, params *OccultationParameters, fr *fitResult, targetTimes, targetValues []float64) error {
	plotImg, err := createNCCPlotImage(fr.nccCurve, 1000, 500)
	if err != nil {
		return fmt.Errorf("failed to create NCC plot: %w", err)
	}

	nccWindow := app.NewWindow("NCC Fit Result")
	plotCanvas := canvas.NewImageFromImage(plotImg)
	plotCanvas.FillMode = canvas.ImageFillOriginal
	nccWindow.SetContent(container.NewScroll(plotCanvas))
	nccWindow.Resize(fyne.NewSize(1050, 550))
	nccWindow.Show()

	fmt.Printf("Best NCC fit: offset=%.4f sec, NCC=%.6f\n", fr.bestShift, fr.bestNCC)

	overlayImg, err := createOverlayPlotImage(fr.curve, fr.bestShift, fr.edgeTimes, targetTimes, targetValues, fr.bestNCC, 1200, 500)
	if err != nil {
		return fmt.Errorf("failed to create overlay plot: %w", err)
	}

	overlayWindow := app.NewWindow("Fit Result — Theoretical vs Observed")
	overlayCanvas := canvas.NewImageFromImage(overlayImg)
	overlayCanvas.FillMode = canvas.ImageFillContain
	overlayWindow.SetContent(overlayCanvas)
	overlayWindow.Resize(fyne.NewSize(1250, 550))
	overlayWindow.Show()

	displayImg, err := lightcurve.LoadImageFromFile("diffractionImage8bit.png")
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
				pathWindow := app.NewWindow(fmt.Sprintf("Observation Path on Diffraction Image (offset=%.3f km)", params.PathPerpendicularOffsetKm))
				pathCanvas := canvas.NewImageFromImage(annotatedImg)
				pathCanvas.FillMode = canvas.ImageFillContain
				pathWindow.SetContent(container.NewScroll(pathCanvas))
				pathWindow.Resize(fyne.NewSize(600, 600))
				pathWindow.Show()
			}
		}
	}

	// Display edge times in a popup dialog
	if len(fr.edgeTimes) > 0 {
		msg := fmt.Sprintf("Best fit: NCC=%.4f, time offset=%.4f sec\n", fr.bestNCC, fr.bestShift)
		msg += fmt.Sprintf("Path offset=%.3f km\n\n", params.PathPerpendicularOffsetKm)
		msg += "Edge times (seconds):\n"
		for i, et := range fr.edgeTimes {
			edgeAbsTime := et + fr.bestShift
			msg += fmt.Sprintf("  Edge %d: %.4f\n", i+1, edgeAbsTime)
		}
		if len(fr.edgeTimes) == 2 {
			duration := math.Abs((fr.edgeTimes[1] + fr.bestShift) - (fr.edgeTimes[0] + fr.bestShift))
			msg += fmt.Sprintf("\nEvent duration: %.4f sec\n", duration)
		}
		fmt.Print(msg)
		edgeLabel := widget.NewLabel(msg)
		edgeLabel.Wrapping = fyne.TextWrapWord
		spacer := canvas.NewRectangle(color.Transparent)
		spacer.SetMinSize(fyne.NewSize(750, 0))
		edgeContainer := container.NewVBox(spacer, edgeLabel)
		dialog.ShowCustom("Fit Edge Times", "OK", edgeContainer, w)
	}

	return nil
}

// performFit runs the NCC sliding fit between the theoretical diffraction light curve
// and the observed target curve, then displays the results in popup windows.
func performFit(app fyne.App, w fyne.Window, params *OccultationParameters, targetTimes, targetValues []float64) error {
	fr, err := runSingleFit(params, targetTimes, targetValues)
	if err != nil {
		return err
	}
	return displayFitResult(app, w, params, fr, targetTimes, targetValues)
}

// fitSearchResult holds the output of runFitSearch for display.
type fitSearchResult struct {
	results        []searchResult
	bestIdx        int
	bestPathOffset float64
}

// runFitSearch computes the NCC fit for a range of path perpendicular offsets.
// It is safe to call from a goroutine. UI display is handled separately.
func runFitSearch(params *OccultationParameters, targetTimes, targetValues []float64, initialOffset, finalOffset float64, numSteps int, onProgress func(float64)) (*fitSearchResult, error) {
	results := make([]searchResult, 0, numSteps)

	for step := 0; step < numSteps; step++ {
		var offset float64
		if numSteps == 1 {
			offset = initialOffset
		} else {
			offset = initialOffset + float64(step)*(finalOffset-initialOffset)/float64(numSteps-1)
		}

		// Set the path offset for this iteration
		params.PathPerpendicularOffsetKm = offset

		fr, err := runSingleFit(params, targetTimes, targetValues)
		if err != nil {
			fmt.Printf("Fit at path offset %.3f km failed: %v\n", offset, err)
			continue
		}

		fmt.Printf("Path offset %.3f km: peak NCC=%.6f at time shift=%.4f sec\n", offset, fr.bestNCC, fr.bestShift)
		results = append(results, searchResult{pathOffset: offset, peakNCC: fr.bestNCC, fr: fr})

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
func displayFitSearchResult(app fyne.App, w fyne.Window, params *OccultationParameters, fsr *fitSearchResult, targetTimes, targetValues []float64) error {
	searchPlotImg, err := createPathOffsetPlotImage(fsr.results, 1000, 500)
	if err != nil {
		return fmt.Errorf("failed to create path offset plot: %w", err)
	}

	searchWindow := app.NewWindow("Peak NCC vs Path Offset")
	searchCanvas := canvas.NewImageFromImage(searchPlotImg)
	searchCanvas.FillMode = canvas.ImageFillOriginal
	searchWindow.SetContent(container.NewScroll(searchCanvas))
	searchWindow.Resize(fyne.NewSize(1050, 550))
	searchWindow.Show()

	params.PathPerpendicularOffsetKm = fsr.bestPathOffset
	return displayFitResult(app, w, params, fsr.results[fsr.bestIdx].fr, targetTimes, targetValues)
}

// searchResult holds results for one path offset in a search.
type searchResult struct {
	pathOffset float64
	peakNCC    float64
	fr         *fitResult
}

// createPathOffsetPlotImage renders peak NCC versus path offset.
func createPathOffsetPlotImage(results []searchResult, plotWidth, plotHeight int) (image.Image, error) {
	plt := plot.New()

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
func createNCCPlotImage(results []nccResult, plotWidth, plotHeight int) (image.Image, error) {
	plt := plot.New()

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
func createOverlayPlotImage(curve []timeIntensityPoint, bestOffset float64, edgeTimes []float64, targetTimes, targetValues []float64, bestNCC float64, plotWidth, plotHeight int) (image.Image, error) {
	plt := plot.New()

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

	plt.Title.Text = fmt.Sprintf("Fit Result (NCC=%.4f, offset=%.3fs)", bestNCC, bestOffset)
	plt.X.Label.Text = "Time (seconds)"
	plt.Y.Label.Text = "Intensity"

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
	theoryLine.Color = color.RGBA{R: 220, G: 30, B: 30, A: 255}
	theoryLine.Width = vg.Points(2)
	plt.Add(theoryLine)

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

	// Legend
	plt.Legend.Add("Observed", targetLine)
	plt.Legend.Add("Theoretical (fit)", theoryLine)
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
	goImg, err := png.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		return nil, fmt.Errorf("failed to decode overlay PNG: %w", err)
	}

	return goImg, nil
}

// createNoiseHistogramImage renders a histogram of noise values with a Gaussian fit overlay.
// Returns the image, mean, and sigma.
func createNoiseHistogramImage(noise []float64, plotWidth, plotHeight int) (image.Image, float64, float64, error) {
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
	histImg, err := png.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to decode histogram PNG: %w", err)
	}

	return histImg, mean, sigma, nil
}
