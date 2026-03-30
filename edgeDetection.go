package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
	"gonum.org/v1/plot/vg/vgimg"

	"GoPyOTE/lightcurve"
)

const (
	// Edge detection parameters for diffraction image shadow boundary detection.
	edgeDetectStepSize       = 0.25 // sub-pixel sampling step along the path
	edgeDetectFilterSigma    = 1.0  // Gaussian smoothing sigma
	edgeDetectThresholdLow   = 0.3  // hysteresis lower threshold (outside → inside)
	edgeDetectThresholdHigh  = 0.7  // hysteresis upper threshold (inside → outside)
	edgeDetectGrazeThreshold = 0.2  // minimum edge strength for grazing events

	// crossingInterpolationTolerance prevents division by near-zero in edge
	// crossing interpolation.
	crossingInterpolationTolerance = 1e-12
)

type Point struct {
	X, Y float64
}

// edgePlotData holds the data needed to show edge detection diagnostic plots.
type edgePlotData struct {
	svals         []float64
	raw           []float64
	filt          []float64
	threshold     float64
	thresholdLow  float64
	thresholdHigh float64
	crossings     []Crossing
}

// lastEdgePlotData stores the most recent edge detection plot data for deferred display.
var lastEdgePlotData *edgePlotData

// showEdgePlots displays the full and zoomed edge detection plots from the
// most recent edge detection run. Call this on the main thread after the fit
//
//	completes, so the windows persist. Clears the stored data after showing.
func showEdgePlots() {
	d := lastEdgePlotData
	lastEdgePlotData = nil
	if d == nil {
		return
	}
	showRawFiltPlot(d.svals, d.raw, d.filt, d.thresholdLow, d.thresholdHigh)
	if len(d.crossings) > 0 {
		showRawFiltPlotZoomed(d.svals, d.raw, d.filt, d.thresholdLow, d.thresholdHigh, d.crossings[0].S)
	}
}

type Crossing struct {
	S        float64
	X, Y     float64
	Kind     string // "crossing" or "graze"
	Strength float64
}

//
// ===== FAST PIXEL ACCESS =====
//

// insideFast returns 1.0 for black (0), 0.0 for white (255).
func insideFast(img *image.Gray, x, y int) float64 {
	b := img.Bounds()
	if x < b.Min.X || x >= b.Max.X || y < b.Min.Y || y >= b.Max.Y {
		return 0.0
	}

	i := (y-b.Min.Y)*img.Stride + (x - b.Min.X)
	if img.Pix[i] == 0 {
		return 1.0
	}
	return 0.0
}

// bilinear sampling with direct Pix access
func bilinearSampleGrayFast(img *image.Gray, x, y float64) float64 {
	b := img.Bounds()

	if x < float64(b.Min.X) || y < float64(b.Min.Y) ||
		x > float64(b.Max.X-1) || y > float64(b.Max.Y-1) {
		return 0.0
	}

	x0 := int(math.Floor(x))
	y0 := int(math.Floor(y))
	x1 := x0 + 1
	y1 := y0 + 1

	if x1 >= b.Max.X {
		x1 = b.Max.X - 1
	}
	if y1 >= b.Max.Y {
		y1 = b.Max.Y - 1
	}

	fx := x - float64(x0)
	fy := y - float64(y0)

	v00 := insideFast(img, x0, y0)
	v10 := insideFast(img, x1, y0)
	v01 := insideFast(img, x0, y1)
	v11 := insideFast(img, x1, y1)

	return (1-fx)*(1-fy)*v00 +
		fx*(1-fy)*v10 +
		(1-fx)*fy*v01 +
		fx*fy*v11
}

//
// ===== FILTER =====
//

func gaussianKernel1D(ds, sigma float64) []float64 {
	if sigma <= 0 {
		return []float64{1}
	}

	half := int(math.Ceil(3 * sigma / ds))
	n := 2*half + 1
	kernel := make([]float64, n)

	sum := 0.0
	for i := -half; i <= half; i++ {
		x := float64(i) * ds
		v := math.Exp(-(x * x) / (2 * sigma * sigma))
		kernel[i+half] = v
		sum += v
	}

	for i := range kernel {
		kernel[i] /= sum
	}
	return kernel
}

func convolveSame(signal, kernel []float64) []float64 {
	n := len(signal)
	m := len(kernel)
	half := m / 2
	out := make([]float64, n)

	for i := 0; i < n; i++ {
		sum := 0.0
		wsum := 0.0
		for k := 0; k < m; k++ {
			j := i + k - half
			if j < 0 || j >= n {
				continue
			}
			sum += signal[j] * kernel[k]
			wsum += kernel[k]
		}
		if wsum > 0 {
			out[i] = sum / wsum
		}
	}
	return out
}

//
// ===== MAIN ROUTINE =====
//

func DetectRobustCrossingsGrayFast(
	img *image.Gray,
	p0, p1 Point,
	ds float64,
	sigma float64,
	thresholdLow float64,
	thresholdHigh float64,
	grazeThreshold float64,
	showPlots ...bool,
) []Crossing {

	dx := p1.X - p0.X
	dy := p1.Y - p0.Y
	L := math.Hypot(dx, dy)
	if L == 0 {
		return nil
	}

	n := int(math.Ceil(L/ds)) + 1

	svals := make([]float64, n)
	raw := make([]float64, n)
	pts := make([]Point, n)

	// sample along the path
	for i := 0; i < n; i++ {
		s := float64(i) * ds
		if s > L {
			s = L
		}
		t := s / L

		x := p0.X + t*dx
		y := p0.Y + t*dy

		svals[i] = s
		pts[i] = Point{x, y}
		raw[i] = bilinearSampleGrayFast(img, x, y)
	}

	// filter
	kernel := gaussianKernel1D(ds, sigma)
	filt := convolveSame(raw, kernel)
	// detect crossings using hysteresis
	// Outside: filt <= thresholdLow
	// Inside:  filt >= thresholdHigh
	// Between them: transition band
	midThreshold := 0.5 * (thresholdLow + thresholdHigh)

	merged := make([]Crossing, 0)

	type State int
	const (
		Unknown State = iota
		Outside
		Inside
	)

	state := Unknown

	// initialize state from the first sample that is clearly inside or outside
	startIdx := 0
	for i := 0; i < n; i++ {
		if filt[i] <= thresholdLow {
			state = Outside
			startIdx = i
			break
		}
		if filt[i] >= thresholdHigh {
			state = Inside
			startIdx = i
			break
		}
	}

	bandStart := -1
	prevState := state // track confirmed state before entering the transition band

	for i := startIdx; i < n; i++ {
		switch state {

		case Outside:
			// wait until we enter the transition band
			if filt[i] > thresholdLow {
				bandStart = i
				prevState = Outside
				state = Unknown
			}

		case Inside:
			// wait until we enter the transition band
			if filt[i] < thresholdHigh {
				bandStart = i
				prevState = Inside
				state = Unknown
			}

		case Unknown:
			// Confirm transition to Inside — only record a crossing if we came from Outside
			if filt[i] >= thresholdHigh {
				if prevState == Outside && bandStart >= 0 {
					// locate the crossing at the mid-threshold inside the band
					crossIdx := -1
					for j := bandStart; j < i; j++ {
						if filt[j] <= midThreshold && filt[j+1] > midThreshold {
							crossIdx = j
							break
						}
					}
					if crossIdx >= 0 {
						den := filt[crossIdx+1] - filt[crossIdx]
						alpha := 0.0
						if math.Abs(den) > crossingInterpolationTolerance {
							alpha = (midThreshold - filt[crossIdx]) / den
						}
						s := svals[crossIdx] + alpha*(svals[crossIdx+1]-svals[crossIdx])
						x := pts[crossIdx].X + alpha*(pts[crossIdx+1].X-pts[crossIdx].X)
						y := pts[crossIdx].Y + alpha*(pts[crossIdx+1].Y-pts[crossIdx].Y)

						merged = append(merged, Crossing{
							S:        s,
							X:        x,
							Y:        y,
							Kind:     "crossing",
							Strength: midThreshold,
						})
					}
				}
				state = Inside
				bandStart = -1

				// Confirm transition to Outside — only record a crossing if we came from Inside
			} else if filt[i] <= thresholdLow {
				if prevState == Inside && bandStart >= 0 {
					// locate the crossing at the mid-threshold inside the band
					crossIdx := -1
					for j := bandStart; j < i; j++ {
						if filt[j] >= midThreshold && filt[j+1] < midThreshold {
							crossIdx = j
							break
						}
					}
					if crossIdx >= 0 {
						den := filt[crossIdx+1] - filt[crossIdx]
						alpha := 0.0
						if math.Abs(den) > crossingInterpolationTolerance {
							alpha = (midThreshold - filt[crossIdx]) / den
						}
						s := svals[crossIdx] + alpha*(svals[crossIdx+1]-svals[crossIdx])
						x := pts[crossIdx].X + alpha*(pts[crossIdx+1].X-pts[crossIdx].X)
						y := pts[crossIdx].Y + alpha*(pts[crossIdx+1].Y-pts[crossIdx].Y)

						merged = append(merged, Crossing{
							S:        s,
							X:        x,
							Y:        y,
							Kind:     "crossing",
							Strength: midThreshold,
						})
					}
				}
				state = Outside
				bandStart = -1
			}
		}
	}

	// graze fallback
	if len(merged) == 0 {
		bestIdx := -1
		bestVal := -1.0

		for i := 1; i < n-1; i++ {
			if filt[i] >= filt[i-1] && filt[i] >= filt[i+1] && filt[i] > bestVal {
				bestVal = filt[i]
				bestIdx = i
			}
		}

		if bestIdx >= 0 && bestVal >= grazeThreshold {
			merged = append(merged, Crossing{
				S:        svals[bestIdx],
				X:        pts[bestIdx].X,
				Y:        pts[bestIdx].Y,
				Kind:     "graze",
				Strength: bestVal,
			})
		}
	}

	// Store plot data for deferred display via showEdgePlots()
	wantPlots := len(showPlots) == 0 || showPlots[0]
	if wantPlots {
		lastEdgePlotData = &edgePlotData{
			svals:         svals,
			raw:           raw,
			filt:          filt,
			threshold:     midThreshold,
			thresholdLow:  thresholdLow,
			thresholdHigh: thresholdHigh,
			crossings:     merged,
		}
	}

	return merged
}

//
// ===== ADAPTER: replace FindEdgesInGeometricShadow =====
//

// loadGeometricShadowGray loads a geometric shadow PNG as *image.Gray.
// If the source is not already Gray, it converts via luminance.
func loadGeometricShadowGray(path string) (*image.Gray, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			fmt.Printf("Warning: failed to close geometric shadow file: %v\n", cerr)
		}
	}()

	src, err := png.Decode(f)
	if err != nil {
		return nil, err
	}

	if g, ok := src.(*image.Gray); ok {
		return g, nil
	}

	// Convert to Gray
	b := src.Bounds()
	gray := image.NewGray(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := src.At(x, y).RGBA()
			lum := (r + g + bl) / 3 / 256
			gray.SetGray(x, y, color.Gray{Y: uint8(lum)})
		}
	}
	return gray, nil
}

// findEdgesRobust is a drop-in replacement for lightcurve.FindEdgesInGeometricShadow.
// It loads the geometric shadow as *image.Gray, maps the ObservationPath endpoints to
// Point pairs, calls DetectRobustCrossingsGrayFast with sensible defaults, and returns
// edge distances from path start in pixel units (the same format the caller expects).
// showPlots controls whether diagnostic plots are displayed (default: false).
func findEdgesRobust(geometricShadowPath string, path *lightcurve.ObservationPath, showPlots ...bool) ([]float64, error) {
	img, err := loadGeometricShadowGray(geometricShadowPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load geometric shadow: %w", err)
	}

	if len(path.SamplePoints) == 0 {
		path.ComputeSamplePoints()
	}

	p0 := Point{X: path.StartX, Y: path.StartY}
	p1 := Point{X: path.EndX, Y: path.EndY}

	show := len(showPlots) > 0 && showPlots[0]
	crossings := DetectRobustCrossingsGrayFast(
		img,
		p0, p1,
		edgeDetectStepSize,
		edgeDetectFilterSigma,
		edgeDetectThresholdLow,
		edgeDetectThresholdHigh,
		edgeDetectGrazeThreshold,
		show,
	)

	edges := make([]float64, len(crossings))
	for i, c := range crossings {
		edges[i] = c.S
	}
	return edges, nil
}

// showRawFiltPlotZoomed creates a plot of raw and filtered signals zoomed to a
// window around crossingS (the distance of the first edge crossing).
func showRawFiltPlotZoomed(svals, raw, filt []float64, thresholdLow, thresholdHigh, crossingS float64) {
	// Zoom to ±50 pixels around the crossing
	margin := 10.0
	sMin := crossingS - margin
	sMax := crossingS + margin

	// Find the index range within the zoom window
	lo, hi := -1, -1
	for i, s := range svals {
		if s >= sMin && lo < 0 {
			lo = i
		}
		if s <= sMax {
			hi = i
		}
	}
	if lo < 0 || hi < lo {
		return
	}

	zSvals := svals[lo : hi+1]
	zRaw := raw[lo : hi+1]
	zFilt := filt[lo : hi+1]

	plt := plot.New()
	plt.Title.Text = fmt.Sprintf("Edge Detection — Zoomed at first edge (s=%.1f)", crossingS)
	plt.X.Label.Text = "Distance along path (pixels)"
	plt.Y.Label.Text = "Value"
	plt.Add(plotter.NewGrid())

	// Raw signal
	rawPts := make(plotter.XYs, len(zSvals))
	for i := range zSvals {
		rawPts[i].X = zSvals[i]
		rawPts[i].Y = zRaw[i]
	}
	rawLine, err := plotter.NewLine(rawPts)
	if err == nil {
		rawLine.Color = color.RGBA{R: 180, G: 180, B: 180, A: 255}
		rawLine.Width = vg.Points(1)
		plt.Add(rawLine)
		plt.Legend.Add("Raw", rawLine)
	}

	// Filtered signal
	filtPts := make(plotter.XYs, len(zSvals))
	for i := range zSvals {
		filtPts[i].X = zSvals[i]
		filtPts[i].Y = zFilt[i]
	}
	filtLine, err := plotter.NewLine(filtPts)
	if err == nil {
		filtLine.Color = color.RGBA{R: 0, G: 0, B: 200, A: 255}
		filtLine.Width = vg.Points(1.5)
		plt.Add(filtLine)
		plt.Legend.Add("Filtered", filtLine)
	}

	// Threshold lines
	xMin, xMax := zSvals[0], zSvals[len(zSvals)-1]
	lowPts := plotter.XYs{{X: xMin, Y: thresholdLow}, {X: xMax, Y: thresholdLow}}
	lowLine, err := plotter.NewLine(lowPts)
	if err == nil {
		lowLine.Color = color.RGBA{R: 200, G: 120, B: 0, A: 255}
		lowLine.Width = vg.Points(1)
		lowLine.Dashes = []vg.Length{vg.Points(4), vg.Points(3)}
		plt.Add(lowLine)
		plt.Legend.Add(fmt.Sprintf("Thresh-Low (%.2f)", thresholdLow), lowLine)
	}
	highPts := plotter.XYs{{X: xMin, Y: thresholdHigh}, {X: xMax, Y: thresholdHigh}}
	highLine, err := plotter.NewLine(highPts)
	if err == nil {
		highLine.Color = color.RGBA{R: 200, G: 0, B: 0, A: 255}
		highLine.Width = vg.Points(1)
		highLine.Dashes = []vg.Length{vg.Points(4), vg.Points(3)}
		plt.Add(highLine)
		plt.Legend.Add(fmt.Sprintf("Thresh-High (%.2f)", thresholdHigh), highLine)
	}

	// Vertical line at crossing
	crossPts := plotter.XYs{
		{X: crossingS, Y: 0},
		{X: crossingS, Y: 1},
	}
	crossLine, err := plotter.NewLine(crossPts)
	if err == nil {
		crossLine.Color = color.RGBA{R: 0, G: 160, B: 0, A: 255}
		crossLine.Width = vg.Points(1.5)
		crossLine.Dashes = []vg.Length{vg.Points(4), vg.Points(3)}
		plt.Add(crossLine)
		plt.Legend.Add(fmt.Sprintf("Crossing (s=%.1f)", crossingS), crossLine)
	}

	plt.Legend.Top = true

	width := vg.Length(1000) * vg.Inch / 96
	height := vg.Length(500) * vg.Inch / 96
	imgCanvas := vgimg.New(width, height)
	dc := draw.New(imgCanvas)
	plt.Draw(dc)

	var buf bytes.Buffer
	if err := png.Encode(&buf, imgCanvas.Image()); err != nil {
		fmt.Printf("Failed to encode zoomed edge plot: %v\n", err)
		return
	}
	plotImg, _, err := image.Decode(&buf)
	if err != nil {
		fmt.Printf("Failed to decode zoomed edge plot: %v\n", err)
		return
	}

	app := fyne.CurrentApp()
	win := app.NewWindow("Edge Detection — Zoomed at First Edge")
	imgWidget := canvas.NewImageFromImage(plotImg)
	imgWidget.FillMode = canvas.ImageFillOriginal
	win.SetContent(container.NewScroll(imgWidget))
	win.Resize(fyne.NewSize(1050, 550))
	win.CenterOnScreen()
	win.Show()
}

// showRawFiltPlot creates a gonum/plot of the raw and filtered signals and
// displays it in a Fyne window.
func showRawFiltPlot(svals, raw, filt []float64, thresholdLow, thresholdHigh float64) {
	plt := plot.New()
	plt.Title.Text = "Edge Detection — Raw & Filtered Signal"
	plt.X.Label.Text = "Distance along path (pixels)"
	plt.Y.Label.Text = "Value"
	plt.Add(plotter.NewGrid())

	// Raw signal
	rawPts := make(plotter.XYs, len(svals))
	for i := range svals {
		rawPts[i].X = svals[i]
		rawPts[i].Y = raw[i]
	}
	rawLine, err := plotter.NewLine(rawPts)
	if err == nil {
		rawLine.Color = color.RGBA{R: 180, G: 180, B: 180, A: 255}
		rawLine.Width = vg.Points(1)
		plt.Add(rawLine)
		plt.Legend.Add("Raw", rawLine)
	}

	// Filtered signal
	filtPts := make(plotter.XYs, len(svals))
	for i := range svals {
		filtPts[i].X = svals[i]
		filtPts[i].Y = filt[i]
	}
	filtLine, err := plotter.NewLine(filtPts)
	if err == nil {
		filtLine.Color = color.RGBA{R: 0, G: 0, B: 200, A: 255}
		filtLine.Width = vg.Points(1.5)
		plt.Add(filtLine)
		plt.Legend.Add("Filtered", filtLine)
	}

	// Threshold lines
	xMin, xMax := svals[0], svals[len(svals)-1]
	lowPts := plotter.XYs{{X: xMin, Y: thresholdLow}, {X: xMax, Y: thresholdLow}}
	lowLine, err := plotter.NewLine(lowPts)
	if err == nil {
		lowLine.Color = color.RGBA{R: 200, G: 120, B: 0, A: 255}
		lowLine.Width = vg.Points(1)
		lowLine.Dashes = []vg.Length{vg.Points(4), vg.Points(3)}
		plt.Add(lowLine)
		plt.Legend.Add(fmt.Sprintf("Thresh-Low (%.2f)", thresholdLow), lowLine)
	}
	highPts := plotter.XYs{{X: xMin, Y: thresholdHigh}, {X: xMax, Y: thresholdHigh}}
	highLine, err := plotter.NewLine(highPts)
	if err == nil {
		highLine.Color = color.RGBA{R: 200, G: 0, B: 0, A: 255}
		highLine.Width = vg.Points(1)
		highLine.Dashes = []vg.Length{vg.Points(4), vg.Points(3)}
		plt.Add(highLine)
		plt.Legend.Add(fmt.Sprintf("Thresh-High (%.2f)", thresholdHigh), highLine)
	}

	plt.Legend.Top = true

	width := vg.Length(1000) * vg.Inch / 96
	height := vg.Length(500) * vg.Inch / 96
	imgCanvas := vgimg.New(width, height)
	dc := draw.New(imgCanvas)
	plt.Draw(dc)

	var buf bytes.Buffer
	if err := png.Encode(&buf, imgCanvas.Image()); err != nil {
		fmt.Printf("Failed to encode edge detection plot: %v\n", err)
		return
	}
	plotImg, _, err := image.Decode(&buf)
	if err != nil {
		fmt.Printf("Failed to decode edge detection plot: %v\n", err)
		return
	}

	app := fyne.CurrentApp()
	win := app.NewWindow("Edge Detection — Raw & Filtered")
	imgWidget := canvas.NewImageFromImage(plotImg)
	imgWidget.FillMode = canvas.ImageFillOriginal
	win.SetContent(container.NewScroll(imgWidget))
	win.Resize(fyne.NewSize(1050, 550))
	win.CenterOnScreen()
	win.Show()
}
