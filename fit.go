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

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
	"gonum.org/v1/plot/vg/vgimg"
)

// nccResult holds a single time-offset and its NCC score.
type nccResult struct {
	offset float64
	ncc    float64
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

// performFit runs the NCC sliding fit between the theoretical diffraction light curve
// and the observed target curve, then displays the results in a popup plot window.
func performFit(app fyne.App, _ fyne.Window, params *OccultationParameters, targetTimes, targetValues []float64) error {
	// --- Generate a theoretical curve (same pattern as btnExtractCSV) ---
	lcData, _, err := lightcurve.ExtractAndPlotLightCurve(
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
		return fmt.Errorf("failed to extract diffraction light curve: %w", err)
	}
	if len(lcData) == 0 {
		return fmt.Errorf("no diffraction light curve data extracted")
	}

	shadowSpeed := math.Sqrt(params.DXKmPerSec*params.DXKmPerSec + params.DYKmPerSec*params.DYKmPerSec)
	if shadowSpeed == 0 {
		return fmt.Errorf("shadow speed is zero — check dX and dY parameters")
	}

	// Convert distance to time starting at 0
	kmStart := lcData[0].Distance
	curve := make([]timeIntensityPoint, len(lcData))
	for i, pt := range lcData {
		curve[i] = timeIntensityPoint{
			time:      (pt.Distance - kmStart) / shadowSpeed,
			intensity: pt.Intensity,
		}
	}

	// Apply camera exposure integration
	curve = applyCameraExposure(curve, params.ExposureTimeSecs)

	// Theoretical duration
	theoreticalDuration := curve[len(curve)-1].time

	// Pre-extract sorted times for binary search in interpolateAt
	curveTimes := make([]float64, len(curve))
	for i, pt := range curve {
		curveTimes[i] = pt.time
	}

	// --- Compute a frame period as median of consecutive time gaps ---
	framePeriod := medianTimeDelta(targetTimes)
	if framePeriod <= 0 {
		return fmt.Errorf("could not determine frame period from target timestamps")
	}

	// --- Sliding NCC loop ---
	// Shift range: slide the theoretical curve across the target observation window
	// At shift s, the theoretical curve occupies [s, s + theoreticalDuration] in target time.
	// Initial: theoretical last point aligns with first target frame
	// Final: theoretical first point aligns with the last target frame
	shiftStart := targetTimes[0] - theoreticalDuration
	shiftEnd := targetTimes[len(targetTimes)-1]

	numSteps := int((shiftEnd-shiftStart)/framePeriod) + 1
	results := make([]nccResult, 0, numSteps)

	sampled := make([]float64, len(targetTimes))

	for step := 0; step < numSteps; step++ {
		shift := shiftStart + float64(step)*framePeriod

		// Sample the theoretical curve at each target frame time
		for i, t := range targetTimes {
			localT := t - shift
			if localT < 0 || localT > theoreticalDuration {
				sampled[i] = 1.0 // Outside theoretical curve = baseline
			} else {
				sampled[i] = interpolateAt(curve, curveTimes, localT)
			}
		}

		ncc := computeNCC(targetValues, sampled)
		results = append(results, nccResult{offset: shift, ncc: ncc})
	}

	if len(results) == 0 {
		return fmt.Errorf("no NCC results computed")
	}

	// --- Display results ---
	plotImg, err := createNCCPlotImage(results, 1000, 500)
	if err != nil {
		return fmt.Errorf("failed to create NCC plot: %w", err)
	}

	nccWindow := app.NewWindow("NCC Fit Result")
	plotCanvas := canvas.NewImageFromImage(plotImg)
	plotCanvas.FillMode = canvas.ImageFillOriginal
	nccWindow.SetContent(container.NewScroll(plotCanvas))
	nccWindow.Resize(fyne.NewSize(1050, 550))
	nccWindow.Show()

	// Log the best fit
	bestIdx := 0
	for i, r := range results {
		if r.ncc > results[bestIdx].ncc {
			bestIdx = i
		}
	}
	fmt.Printf("Best NCC fit: offset=%.4f sec, NCC=%.6f\n", results[bestIdx].offset, results[bestIdx].ncc)

	return nil
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

	// Build XY data for line
	pts := make(plotter.XYs, len(results))
	bestIdx := 0
	for i, r := range results {
		pts[i].X = r.offset
		pts[i].Y = r.ncc
		if r.ncc > results[bestIdx].ncc {
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

	// Highlight peak with red scatter point
	peakPt := make(plotter.XYs, 1)
	peakPt[0].X = results[bestIdx].offset
	peakPt[0].Y = results[bestIdx].ncc
	peakScatter, err := plotter.NewScatter(peakPt)
	if err == nil {
		peakScatter.Color = color.RGBA{R: 255, G: 0, B: 0, A: 255}
		peakScatter.GlyphStyle.Shape = draw.CircleGlyph{}
		peakScatter.GlyphStyle.Radius = vg.Points(5)
		plt.Add(peakScatter)
	}

	// Add peak annotation to legend
	plt.Legend.Add(fmt.Sprintf("Peak: offset=%.3fs, NCC=%.4f", results[bestIdx].offset, results[bestIdx].ncc), line)
	plt.Legend.Top = true
	plt.Legend.Left = false

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
