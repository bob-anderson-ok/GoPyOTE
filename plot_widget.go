package main

import (
	"bytes"
	"fmt"
	"image/color"
	"image/png"
	"math"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
	"gonum.org/v1/plot/vg/vgimg"
)

// LightCurvePlot is a custom widget for displaying interactive light curve plots using gonum/plot
type LightCurvePlot struct {
	widget.BaseWidget
	series            []PlotSeries
	pointRadius       float32
	onPointClicked    func(point PlotPoint)
	onScroll          func(position fyne.Position, scrollDelta float32) // Callback for scroll events
	onWarning         func(message string)                              // Callback for warnings
	selectedSeries    int
	selectedIndex     int
	selectedSeries2   int
	selectedIndex2    int
	xAxisLabel        string
	useTimestampTicks bool // When true, format X axis ticks as timestamps

	// Stable identifiers for preserving selection across rebuilds
	selectedPointDataIndex  int    // Original data index of selected point 1
	selectedSeriesName      string // Name of the series containing selected point 1
	selectedPointDataIndex2 int    // Original data index of selected point 2
	selectedSeriesName2     string // Name of the series containing selected point 2

	// Selected point values available for later internal use
	SelectedPoint1Valid bool
	SelectedPoint1Frame float64
	SelectedPoint1Value float64
	SelectedPoint2Valid bool
	SelectedPoint2Frame float64
	SelectedPoint2Value float64

	// Selection mode
	SingleSelectMode    bool // When true, only allow single point selection
	MultiPairSelectMode bool // When true, allow multiple two-point pair selections

	// Multiple pair selections (for Fit tab)
	SelectedPairs []PointPair // List of selected point pairs

	// Baseline line (for Fit tab)
	BaselineValue    float64 // Y value for the baseline horizontal line
	ShowBaselineLine bool    // Whether to draw the baseline line

	// Vertical edge lines (for diffraction analysis)
	VerticalLines     []float64 // X values for vertical lines
	ShowVerticalLines bool      // Whether to draw the vertical lines

	// Plot bounds for coordinate conversion
	minX, maxX, minY, maxY float64
	// Plot area margins (in pixels) - approximate values for gonum/plot
	marginLeft, marginRight, marginTop, marginBottom float32
}

// NewLightCurvePlot creates a new light curve plot widget
func NewLightCurvePlot(series []PlotSeries, onPointClicked func(PlotPoint)) *LightCurvePlot {
	p := &LightCurvePlot{
		series:                  series,
		pointRadius:             5,
		onPointClicked:          onPointClicked,
		selectedSeries:          -1,
		selectedIndex:           -1,
		selectedSeries2:         -1,
		selectedIndex2:          -1,
		selectedPointDataIndex:  -1,
		selectedSeriesName:      "",
		selectedPointDataIndex2: -1,
		selectedSeriesName2:     "",
		xAxisLabel:              "Time",
		marginLeft:              75,
		marginRight:             15,
		marginTop:               15,
		marginBottom:            45,
	}
	p.calculateBounds()
	p.ExtendBaseWidget(p)
	return p
}

// SetXAxisLabel sets the label for the X axis
func (p *LightCurvePlot) SetXAxisLabel(label string) {
	p.xAxisLabel = label
	p.Refresh()
}

// SetXBounds sets the X axis min and max values
func (p *LightCurvePlot) SetXBounds(minX, maxX float64) {
	if maxX > minX {
		p.minX = minX
		p.maxX = maxX
		p.Refresh()
	}
}

// GetXBounds returns the current X axis min and max values
func (p *LightCurvePlot) GetXBounds() (float64, float64) {
	return p.minX, p.maxX
}

// SetYBounds sets the Y axis min and max values
func (p *LightCurvePlot) SetYBounds(minY, maxY float64) {
	if maxY > minY {
		p.minY = minY
		p.maxY = maxY
		p.updateMargins()
		p.Refresh()
	}
}

// GetYBounds returns the current Y axis min and max values
func (p *LightCurvePlot) GetYBounds() (float64, float64) {
	return p.minY, p.maxY
}

// SetUseTimestampTicks sets whether X axis ticks should be formatted as timestamps
func (p *LightCurvePlot) SetUseTimestampTicks(use bool) {
	p.useTimestampTicks = use
	p.updateMargins()
	p.Refresh()
}

// GetUseTimestampTicks returns whether X axis ticks are formatted as timestamps
func (p *LightCurvePlot) GetUseTimestampTicks() bool {
	return p.useTimestampTicks
}

// SetOnScroll sets the callback for scroll events
func (p *LightCurvePlot) SetOnScroll(callback func(position fyne.Position, scrollDelta float32)) {
	p.onScroll = callback
}

// SetOnWarning sets the callback for warning messages
func (p *LightCurvePlot) SetOnWarning(callback func(message string)) {
	p.onWarning = callback
}

// SetVerticalLines sets the X positions for vertical edge lines
func (p *LightCurvePlot) SetVerticalLines(xValues []float64, show bool) {
	p.VerticalLines = xValues
	p.ShowVerticalLines = show
	p.Refresh()
}

// Scrolled handles scroll wheel events
func (p *LightCurvePlot) Scrolled(ev *fyne.ScrollEvent) {
	if p.onScroll != nil {
		p.onScroll(ev.Position, ev.Scrolled.DY)
	}
}

// timestampTicker is a custom tick marker that formats values as timestamps
type timestampTicker struct{}

// Ticks returns tick marks for the given axis range, formatted as timestamps
func (t timestampTicker) Ticks(min, max float64) []plot.Tick {
	// Use the default ticker to get reasonable tick positions
	defaultTicker := plot.DefaultTicks{}
	ticks := defaultTicker.Ticks(min, max)

	// Replace labels with the timestamp format
	for i := range ticks {
		if ticks[i].Label != "" {
			ticks[i].Label = formatSecondsAsTimestamp(ticks[i].Value)
		}
	}
	return ticks
}

// calculateBounds computes the data bounds across all series
func (p *LightCurvePlot) calculateBounds() {
	if len(p.series) == 0 {
		return
	}
	first := true
	for _, s := range p.series {
		for _, pt := range s.Points {
			if first {
				p.minX, p.maxX = pt.X, pt.X
				p.minY, p.maxY = pt.Y, pt.Y
				first = false
			} else {
				if pt.X < p.minX {
					p.minX = pt.X
				}
				if pt.X > p.maxX {
					p.maxX = pt.X
				}
				if pt.Y < p.minY {
					p.minY = pt.Y
				}
				if pt.Y > p.maxY {
					p.maxY = pt.Y
				}
			}
		}
	}
	// Add padding
	rangeX := p.maxX - p.minX
	rangeY := p.maxY - p.minY
	if rangeX == 0 {
		rangeX = 1
	}
	if rangeY == 0 {
		rangeY = 1
	}
	p.minX -= rangeX * 0.05
	p.maxX += rangeX * 0.05
	p.minY -= rangeY * 0.05
	p.maxY += rangeY * 0.05

	// Update margins based on tick label widths
	p.updateMargins()
}

// updateMargins calculates dynamic margins based on tick label widths
func (p *LightCurvePlot) updateMargins() {
	// Estimate left margin based on Y tick label width
	// Find the widest Y value that would be displayed
	maxYStr := fmt.Sprintf("%.0f", p.maxY)
	minYStr := fmt.Sprintf("%.0f", p.minY)
	maxLen := len(maxYStr)
	if len(minYStr) > maxLen {
		maxLen = len(minYStr)
	}
	// Each character is approximately 7 pixels wide, plus padding for axis label
	// Base: 45 pixels for Y axis label and padding, plus ~8 pixels per character
	p.marginLeft = float32(45 + maxLen*8)
	if p.marginLeft < 60 {
		p.marginLeft = 60
	}
	if p.marginLeft > 120 {
		p.marginLeft = 120
	}

	// Estimate bottom margin based on X tick label width
	// For timestamp format (hh:mm:ss.ssss), need more space
	if p.useTimestampTicks {
		p.marginBottom = 55
	} else {
		maxXStr := fmt.Sprintf("%.0f", p.maxX)
		if len(maxXStr) > 6 {
			p.marginBottom = 50
		} else {
			p.marginBottom = 45
		}
	}
}

// SetSeries updates the plot data
func (p *LightCurvePlot) SetSeries(series []PlotSeries) {
	p.series = series
	p.selectedSeries = -1
	p.selectedIndex = -1
	p.selectedSeries2 = -1
	p.selectedIndex2 = -1

	// Try to restore selection 1 based on saved stable identifiers
	// Keep stable identifiers even if the point is off-screen (will restore when zoomed back)
	if p.selectedPointDataIndex >= 0 && p.selectedSeriesName != "" {
		for s, ser := range series {
			if ser.Name == p.selectedSeriesName {
				for i, pt := range ser.Points {
					if pt.Index == p.selectedPointDataIndex {
						p.selectedSeries = s
						p.selectedIndex = i
						p.SelectedPoint1Frame = pt.X
						p.SelectedPoint1Value = pt.Y
						break
					}
				}
				break
			}
		}
		// If not found in the current view, keep stable identifiers but don't highlight
		// Selection will be restored when the point comes back into view
	}

	// Try to restore selection 2 based on saved stable identifiers
	// Keep stable identifiers even if the point is off-screen (will restore when zoomed back)
	if p.selectedPointDataIndex2 >= 0 && p.selectedSeriesName2 != "" {
		for s, ser := range series {
			if ser.Name == p.selectedSeriesName2 {
				for i, pt := range ser.Points {
					if pt.Index == p.selectedPointDataIndex2 {
						p.selectedSeries2 = s
						p.selectedIndex2 = i
						p.SelectedPoint2Frame = pt.X
						p.SelectedPoint2Value = pt.Y
						break
					}
				}
				break
			}
		}
		// If not found in the current view, keep stable identifiers but don't highlight
		// Selection will be restored when the point comes back into view
	}

	p.calculateBounds()
	p.Refresh()
}

// MinSize returns the minimum size
func (p *LightCurvePlot) MinSize() fyne.Size {
	return fyne.NewSize(200, 150)
}

// CreateRenderer creates the plot renderer
func (p *LightCurvePlot) CreateRenderer() fyne.WidgetRenderer {
	return &lightCurvePlotRenderer{plot: p}
}

// Tapped handles tap/click events
func (p *LightCurvePlot) Tapped(ev *fyne.PointEvent) {
	p.handleClick(ev.Position)
}

// MouseDown handles mouse down events for desktop
func (p *LightCurvePlot) MouseDown(ev *desktop.MouseEvent) {
	p.handleClick(ev.Position)
}

// pixelToDataX converts pixel X coordinate to data X coordinate
func pixelToDataX(minX, maxX, px, width float64) float64 {
	return minX + px/width*(maxX-minX)
}

// pixelToDataY converts pixel Y coordinate to data Y coordinate (Y axis is inverted in screen space)
func pixelToDataY(minY, maxY, py, height float64) float64 {
	return maxY - py/height*(maxY-minY)
}

func (p *LightCurvePlot) handleClick(pos fyne.Position) {
	if len(p.series) == 0 {
		return
	}

	size := p.Size()

	// Calculate plot area dimensions
	plotWidth := float64(size.Width - p.marginLeft - p.marginRight)
	plotHeight := float64(size.Height - p.marginTop - p.marginBottom)
	if plotWidth <= 0 {
		plotWidth = 1
	}
	if plotHeight <= 0 {
		plotHeight = 1
	}

	// Convert click position to pixel coordinates relative to the plot area
	clickPx := float64(pos.X - p.marginLeft)
	clickPy := float64(pos.Y - p.marginTop)

	// Convert click to data coordinates
	clickDataX := pixelToDataX(p.minX, p.maxX, clickPx, plotWidth)
	clickDataY := pixelToDataY(p.minY, p.maxY, clickPy, plotHeight)

	// Find the closest point across all series using nearestPoint
	// Work in normalized pixel space (0 to 1) so X and Y have equal weight
	clickNormX := clickPx / plotWidth
	clickNormY := clickPy / plotHeight

	var closestSeries = -1
	var closestIdx = -1
	var bestDistSq = math.MaxFloat64
	tolerance := 0.08 // 8% of plot area

	for s, series := range p.series {
		// Build arrays of normalized pixel coordinates for this series
		xs := make([]float64, len(series.Points))
		ys := make([]float64, len(series.Points))
		for i, pt := range series.Points {
			// Convert data coordinates to normalized pixel coordinates (0 to 1)
			xs[i] = (pt.X - p.minX) / (p.maxX - p.minX)
			ys[i] = (p.maxY - pt.Y) / (p.maxY - p.minY) // Y inverted
		}

		// Find the nearest point in this series
		for i := range xs {
			dx := xs[i] - clickNormX
			dy := ys[i] - clickNormY
			distSq := dx*dx + dy*dy
			if distSq < bestDistSq && distSq < tolerance*tolerance {
				bestDistSq = distSq
				closestSeries = s
				closestIdx = i
			}
		}
	}

	// Log for debugging
	_ = clickDataX
	_ = clickDataY

	if closestSeries >= 0 && closestIdx >= 0 {
		clickedPoint := p.series[closestSeries].Points[closestIdx]

		// If clicking on point 1, deselect it
		if p.selectedSeries == closestSeries && p.selectedIndex == closestIdx {
			p.selectedSeries = -1
			p.selectedIndex = -1
			p.selectedPointDataIndex = -1
			p.selectedSeriesName = ""
			p.SelectedPoint1Valid = false
			p.SelectedPoint1Frame = 0
			p.SelectedPoint1Value = 0
			p.Refresh()
			return
		}

		// Single select mode: just replace point 1
		if p.SingleSelectMode {
			p.selectedSeries = closestSeries
			p.selectedIndex = closestIdx
			p.selectedPointDataIndex = clickedPoint.Index
			p.selectedSeriesName = p.series[closestSeries].Name
			p.SelectedPoint1Valid = true
			p.SelectedPoint1Frame = clickedPoint.X
			p.SelectedPoint1Value = clickedPoint.Y
			p.Refresh()
			if p.onPointClicked != nil {
				p.onPointClicked(clickedPoint)
			}
			return
		}

		// Multi-pair select mode: allow multiple two-point pair selections
		if p.MultiPairSelectMode {
			// Check if clicking on a point that's already part of a saved pair - remove that pair
			for i, pair := range p.SelectedPairs {
				if (pair.Point1SeriesIdx == closestSeries && pair.Point1Idx == closestIdx) ||
					(pair.Point2SeriesIdx == closestSeries && pair.Point2Idx == closestIdx) {
					// Remove this pair
					p.SelectedPairs = append(p.SelectedPairs[:i], p.SelectedPairs[i+1:]...)
					p.Refresh()
					if p.onPointClicked != nil {
						p.onPointClicked(clickedPoint)
					}
					return
				}
			}

			// If clicking on the current point 1, deselect it
			if p.selectedSeries == closestSeries && p.selectedIndex == closestIdx {
				p.selectedSeries = -1
				p.selectedIndex = -1
				p.selectedPointDataIndex = -1
				p.selectedSeriesName = ""
				p.SelectedPoint1Valid = false
				p.SelectedPoint1Frame = 0
				p.SelectedPoint1Value = 0
				p.Refresh()
				return
			}

			// If point 1 is not selected, select as point 1
			if p.selectedSeries < 0 {
				p.selectedSeries = closestSeries
				p.selectedIndex = closestIdx
				p.selectedPointDataIndex = clickedPoint.Index
				p.selectedSeriesName = p.series[closestSeries].Name
				p.SelectedPoint1Valid = true
				p.SelectedPoint1Frame = clickedPoint.X
				p.SelectedPoint1Value = clickedPoint.Y
				p.Refresh()
				if p.onPointClicked != nil {
					p.onPointClicked(clickedPoint)
				}
				return
			}

			// Point 1 is selected, select as point 2 and save the pair
			// Warn if point 2 is on a different light curve than point 1
			if closestSeries != p.selectedSeries {
				if p.onWarning != nil {
					p.onWarning("Point 2 is on a different light curve than Point 1")
				}
			}

			// Create and save the pair
			pair := PointPair{
				Point1SeriesIdx: p.selectedSeries,
				Point1Idx:       p.selectedIndex,
				Point1DataIdx:   p.selectedPointDataIndex,
				Point1Frame:     p.SelectedPoint1Frame,
				Point1Value:     p.SelectedPoint1Value,
				Point1Series:    p.selectedSeriesName,
				Point2SeriesIdx: closestSeries,
				Point2Idx:       closestIdx,
				Point2DataIdx:   clickedPoint.Index,
				Point2Frame:     clickedPoint.X,
				Point2Value:     clickedPoint.Y,
				Point2Series:    p.series[closestSeries].Name,
			}
			p.SelectedPairs = append(p.SelectedPairs, pair)

			// Clear point 1 selection to allow selecting the next pair
			p.selectedSeries = -1
			p.selectedIndex = -1
			p.selectedPointDataIndex = -1
			p.selectedSeriesName = ""
			p.SelectedPoint1Valid = false
			p.SelectedPoint1Frame = 0
			p.SelectedPoint1Value = 0

			p.Refresh()
			if p.onPointClicked != nil {
				p.onPointClicked(clickedPoint)
			}
			return
		}

		// If clicking on point 2, deselect it
		if p.selectedSeries2 == closestSeries && p.selectedIndex2 == closestIdx {
			p.selectedSeries2 = -1
			p.selectedIndex2 = -1
			p.selectedPointDataIndex2 = -1
			p.selectedSeriesName2 = ""
			p.SelectedPoint2Valid = false
			p.SelectedPoint2Frame = 0
			p.SelectedPoint2Value = 0
			p.Refresh()
			return
		}

		// If point 1 is not selected, select as point 1
		if p.selectedSeries < 0 {
			p.selectedSeries = closestSeries
			p.selectedIndex = closestIdx
			p.selectedPointDataIndex = clickedPoint.Index
			p.selectedSeriesName = p.series[closestSeries].Name
			p.SelectedPoint1Valid = true
			p.SelectedPoint1Frame = clickedPoint.X
			p.SelectedPoint1Value = clickedPoint.Y
			p.Refresh()
			if p.onPointClicked != nil {
				p.onPointClicked(clickedPoint)
			}
			return
		}

		// If point 2 is not selected, select as point 2
		if p.selectedSeries2 < 0 {
			// Warn if point 2 is on a different light curve than point 1
			if p.selectedSeries >= 0 && closestSeries != p.selectedSeries {
				if p.onWarning != nil {
					p.onWarning("Point 2 is on a different light curve than Point 1")
				}
			}
			p.selectedSeries2 = closestSeries
			p.selectedIndex2 = closestIdx
			p.selectedPointDataIndex2 = clickedPoint.Index
			p.selectedSeriesName2 = p.series[closestSeries].Name
			p.SelectedPoint2Valid = true
			p.SelectedPoint2Frame = clickedPoint.X
			p.SelectedPoint2Value = clickedPoint.Y
			p.Refresh()
			if p.onPointClicked != nil {
				p.onPointClicked(clickedPoint)
			}
			return
		}

		// Both points are selected, replace point 1
		p.selectedSeries = closestSeries
		p.selectedIndex = closestIdx
		p.selectedPointDataIndex = clickedPoint.Index
		p.selectedSeriesName = p.series[closestSeries].Name
		p.SelectedPoint1Valid = true
		p.SelectedPoint1Frame = clickedPoint.X
		p.SelectedPoint1Value = clickedPoint.Y
		p.Refresh()
		if p.onPointClicked != nil {
			p.onPointClicked(clickedPoint)
		}
	}
}

// lightCurvePlotRenderer renders the plot using gonum/plot
type lightCurvePlotRenderer struct {
	plot    *LightCurvePlot
	image   *canvas.Image
	objects []fyne.CanvasObject
}

func (r *lightCurvePlotRenderer) Destroy() {}

func (r *lightCurvePlotRenderer) Layout(size fyne.Size) {
	if r.image != nil {
		r.image.Resize(size)
	}
}

func (r *lightCurvePlotRenderer) MinSize() fyne.Size {
	return r.plot.MinSize()
}

func (r *lightCurvePlotRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *lightCurvePlotRenderer) Refresh() {
	p := r.plot
	size := p.Size()

	if size.Width < 10 || size.Height < 10 {
		return
	}

	// Create gonum plot
	plt := plot.New()
	// Modify the font fields directly on existing styles
	plt.Title.TextStyle.Font.Typeface = "Liberation"
	plt.Title.TextStyle.Font.Variant = "Sans"
	plt.Title.TextStyle.Font.Size = vg.Points(12)

	plt.X.Label.TextStyle.Font.Typeface = "Liberation"
	plt.X.Label.TextStyle.Font.Variant = "Sans"
	plt.X.Label.TextStyle.Font.Size = vg.Points(12)

	plt.Y.Label.TextStyle.Font.Typeface = "Liberation"
	plt.Y.Label.TextStyle.Font.Variant = "Sans"
	plt.Y.Label.TextStyle.Font.Size = vg.Points(12)

	plt.X.Tick.Label.Font.Typeface = "Liberation"
	plt.X.Tick.Label.Font.Variant = "Sans"
	plt.X.Tick.Label.Font.Size = vg.Points(10)

	plt.Y.Tick.Label.Font.Typeface = "Liberation"
	plt.Y.Tick.Label.Font.Variant = "Sans"
	plt.Y.Tick.Label.Font.Size = vg.Points(10)

	// If no series, show an empty plot
	if len(p.series) == 0 {
		plt.Title.Text = "Light Curve"
		plt.X.Label.Text = p.xAxisLabel
		plt.Y.Label.Text = "Brightness"

		// Render empty plot
		width := vg.Length(size.Width) * vg.Inch / 96
		height := vg.Length(size.Height) * vg.Inch / 96
		img := vgimg.New(width, height)
		dc := draw.New(img)
		plt.Draw(dc)

		var buf bytes.Buffer
		if err := png.Encode(&buf, img.Image()); err != nil {
			fmt.Printf("Error encoding plot image: %v\n", err)
			return
		}
		goImg, err := png.Decode(bytes.NewReader(buf.Bytes()))
		if err != nil {
			fmt.Printf("Error decoding plot image: %v\n", err)
			return
		}

		if r.image == nil {
			r.image = canvas.NewImageFromImage(goImg)
			r.image.FillMode = canvas.ImageFillStretch
			r.objects = []fyne.CanvasObject{r.image}
		} else {
			r.image.Image = goImg
			r.image.Refresh()
		}
		r.image.Resize(size)
		return
	}
	plt.Title.Text = "Light Curve"
	plt.Title.TextStyle.Font.Weight = 2 // Bold
	plt.X.Label.Text = p.xAxisLabel
	plt.X.Label.TextStyle.Font.Weight = 2
	plt.Y.Label.Text = "Brightness"
	plt.Y.Label.TextStyle.Font.Weight = 2

	// Add grid
	plt.Add(plotter.NewGrid())

	// Add each series
	for s, series := range p.series {
		// Separate regular, interpolated, and negative delta points
		var regularPts, interpolatedPts, negativeDeltaPts plotter.XYs
		for _, pt := range series.Points {
			xy := plotter.XY{X: pt.X, Y: pt.Y}
			if pt.Interpolated {
				interpolatedPts = append(interpolatedPts, xy)
			} else if isNegativeDeltaIndex(pt.Index) {
				negativeDeltaPts = append(negativeDeltaPts, xy)
			} else {
				regularPts = append(regularPts, xy)
			}
		}

		// Create XY data for line (includes all points for continuity)
		pts := make(plotter.XYs, len(series.Points))
		for i, pt := range series.Points {
			pts[i].X = pt.X
			pts[i].Y = pt.Y
		}

		// Create line
		line, err := plotter.NewLine(pts)
		if err != nil {
			fmt.Printf("Error creating line plot: %v\n", err)
			continue
		}
		line.Color = series.Color
		line.Width = vg.Points(2)
		plt.Add(line)

		// Create scatter points for regular (non-interpolated) points
		if len(regularPts) > 0 {
			scatter, err := plotter.NewScatter(regularPts)
			if err != nil {
				fmt.Printf("Error creating scatter plot: %v\n", err)
			} else {
				scatter.Color = series.Color
				scatter.GlyphStyle.Shape = draw.CircleGlyph{}
				scatter.GlyphStyle.Radius = vg.Points(4)
				plt.Add(scatter)
			}
		}

		// Create scatter points for interpolated points (dark gray with red circle outline)
		if len(interpolatedPts) > 0 {
			// First, draw a larger red circle as the outline
			redOutline, err := plotter.NewScatter(interpolatedPts)
			if err != nil {
				fmt.Printf("Error creating interpolated outline scatter: %v\n", err)
			} else {
				redOutline.Color = color.RGBA{R: 255, G: 0, B: 0, A: 255} // Red
				redOutline.GlyphStyle.Shape = draw.CircleGlyph{}
				redOutline.GlyphStyle.Radius = vg.Points(6) // Larger for outline effect
				plt.Add(redOutline)
			}

			// Then draw the dark gray circle on top
			interpScatter, err := plotter.NewScatter(interpolatedPts)
			if err != nil {
				fmt.Printf("Error creating interpolated scatter plot: %v\n", err)
			} else {
				interpScatter.Color = color.RGBA{R: 100, G: 100, B: 100, A: 255} // Dark gray
				interpScatter.GlyphStyle.Shape = draw.CircleGlyph{}
				interpScatter.GlyphStyle.Radius = vg.Points(4) // Same size as regular points
				plt.Add(interpScatter)
			}
		}

		// Create scatter points for negative delta points (series color with black circle outline)
		if len(negativeDeltaPts) > 0 {
			// First, draw a larger black circle as the outline
			blackOutline, err := plotter.NewScatter(negativeDeltaPts)
			if err != nil {
				fmt.Printf("Error creating negative delta outline scatter: %v\n", err)
			} else {
				blackOutline.Color = color.RGBA{R: 0, G: 0, B: 0, A: 255} // Black
				blackOutline.GlyphStyle.Shape = draw.CircleGlyph{}
				blackOutline.GlyphStyle.Radius = vg.Points(6) // Larger for outline effect
				plt.Add(blackOutline)
			}

			// Then draw the series-colored circle on top
			negDeltaScatter, err := plotter.NewScatter(negativeDeltaPts)
			if err != nil {
				fmt.Printf("Error creating negative delta scatter plot: %v\n", err)
			} else {
				negDeltaScatter.Color = series.Color
				negDeltaScatter.GlyphStyle.Shape = draw.CircleGlyph{}
				negDeltaScatter.GlyphStyle.Radius = vg.Points(4) // Same size as regular points
				plt.Add(negDeltaScatter)
			}
		}

		// Highlight selected point 1 (red)
		if s == p.selectedSeries && p.selectedIndex >= 0 && p.selectedIndex < len(series.Points) {
			selectedPt := make(plotter.XYs, 1)
			selectedPt[0].X = series.Points[p.selectedIndex].X
			selectedPt[0].Y = series.Points[p.selectedIndex].Y
			selectedScatter, err := plotter.NewScatter(selectedPt)
			if err != nil {
				fmt.Printf("Error creating selected point 1 scatter: %v\n", err)
			} else {
				selectedScatter.Color = color.RGBA{R: 255, G: 50, B: 50, A: 255} // Red
				selectedScatter.GlyphStyle.Shape = draw.CircleGlyph{}
				selectedScatter.GlyphStyle.Radius = vg.Points(7)
				plt.Add(selectedScatter)
			}
		}

		// Highlight selected point 2 (blue)
		if s == p.selectedSeries2 && p.selectedIndex2 >= 0 && p.selectedIndex2 < len(series.Points) {
			selectedPt := make(plotter.XYs, 1)
			selectedPt[0].X = series.Points[p.selectedIndex2].X
			selectedPt[0].Y = series.Points[p.selectedIndex2].Y
			selectedScatter, err := plotter.NewScatter(selectedPt)
			if err != nil {
				fmt.Printf("Error creating selected point 2 scatter: %v\n", err)
			} else {
				selectedScatter.Color = color.RGBA{R: 50, G: 50, B: 255, A: 255} // Blue
				selectedScatter.GlyphStyle.Shape = draw.CircleGlyph{}
				selectedScatter.GlyphStyle.Radius = vg.Points(7)
				plt.Add(selectedScatter)
			}
		}

		// Highlight saved pairs (for multi-pair selection mode)
		pairColors := []color.RGBA{
			{R: 0, G: 200, B: 0, A: 255},    // Green
			{R: 255, G: 165, B: 0, A: 255},  // Orange
			{R: 148, G: 0, B: 211, A: 255},  // Purple
			{R: 0, G: 206, B: 209, A: 255},  // Cyan
			{R: 255, G: 20, B: 147, A: 255}, // Pink
			{R: 139, G: 69, B: 19, A: 255},  // Brown
			{R: 50, G: 205, B: 50, A: 255},  // Lime
			{R: 255, G: 215, B: 0, A: 255},  // Gold
		}
		for pairIdx, pair := range p.SelectedPairs {
			pairColor := pairColors[pairIdx%len(pairColors)]

			// Draw point 1 of this pair if it's in the current series
			if pair.Point1SeriesIdx == s && pair.Point1Idx >= 0 && pair.Point1Idx < len(series.Points) {
				pairPt1 := make(plotter.XYs, 1)
				pairPt1[0].X = series.Points[pair.Point1Idx].X
				pairPt1[0].Y = series.Points[pair.Point1Idx].Y
				pairScatter1, err := plotter.NewScatter(pairPt1)
				if err == nil {
					pairScatter1.Color = pairColor
					pairScatter1.GlyphStyle.Shape = draw.CircleGlyph{}
					pairScatter1.GlyphStyle.Radius = vg.Points(7)
					plt.Add(pairScatter1)
				}
			}

			// Draw point 2 of this pair if it's in the current series
			if pair.Point2SeriesIdx == s && pair.Point2Idx >= 0 && pair.Point2Idx < len(series.Points) {
				pairPt2 := make(plotter.XYs, 1)
				pairPt2[0].X = series.Points[pair.Point2Idx].X
				pairPt2[0].Y = series.Points[pair.Point2Idx].Y
				pairScatter2, err := plotter.NewScatter(pairPt2)
				if err == nil {
					pairScatter2.Color = pairColor
					pairScatter2.GlyphStyle.Shape = draw.CircleGlyph{}
					pairScatter2.GlyphStyle.Radius = vg.Points(7)
					plt.Add(pairScatter2)
				}
			}
		}

		// Add to legend (create a dummy scatter for legend entry)
		legendScatter, _ := plotter.NewScatter(pts)
		if legendScatter != nil {
			legendScatter.Color = series.Color
			legendScatter.GlyphStyle.Shape = draw.CircleGlyph{}
			legendScatter.GlyphStyle.Radius = vg.Points(4)
			plt.Legend.Add(series.Name, line, legendScatter)
		} else {
			plt.Legend.Add(series.Name, line)
		}
	}

	// Draw baseline horizontal line if enabled
	if p.ShowBaselineLine {
		baselinePts := make(plotter.XYs, 2)
		baselinePts[0].X = p.minX
		baselinePts[0].Y = p.BaselineValue
		baselinePts[1].X = p.maxX
		baselinePts[1].Y = p.BaselineValue
		baselineLine, err := plotter.NewLine(baselinePts)
		if err == nil {
			baselineLine.Color = color.RGBA{R: 255, G: 0, B: 0, A: 255} // Red
			baselineLine.Width = vg.Points(2)
			baselineLine.Dashes = []vg.Length{vg.Points(5), vg.Points(3)} // Dashed line
			plt.Add(baselineLine)
		}
	}

	// Draw vertical edge lines if enabled
	if p.ShowVerticalLines {
		for _, xVal := range p.VerticalLines {
			vlinePts := make(plotter.XYs, 2)
			vlinePts[0].X = xVal
			vlinePts[0].Y = p.minY
			vlinePts[1].X = xVal
			vlinePts[1].Y = p.maxY
			vline, err := plotter.NewLine(vlinePts)
			if err == nil {
				vline.Color = color.RGBA{R: 255, G: 0, B: 0, A: 255} // Red
				vline.Width = vg.Points(2)
				vline.Dashes = []vg.Length{vg.Points(6), vg.Points(4)} // Dashed line
				plt.Add(vline)
			}
		}
	}

	// Set axis ranges
	plt.X.Min = p.minX
	plt.X.Max = p.maxX
	plt.Y.Min = p.minY
	plt.Y.Max = p.maxY

	// Use timestamp tick labels if enabled
	if p.useTimestampTicks {
		plt.X.Tick.Marker = timestampTicker{}
	}

	plt.Legend.Top = true
	plt.Legend.Left = true

	// Render to image
	width := vg.Length(size.Width) * vg.Inch / 96 // Convert pixels to inches at 96 DPI
	height := vg.Length(size.Height) * vg.Inch / 96

	img := vgimg.New(width, height)
	dc := draw.New(img)
	plt.Draw(dc)

	// Convert to Go image
	var buf bytes.Buffer
	if err := png.Encode(&buf, img.Image()); err != nil {
		fmt.Printf("Error encoding plot image: %v\n", err)
		return
	}
	goImg, err := png.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		fmt.Printf("Error decoding plot image: %v\n", err)
		return
	}

	// Create or update Fyne image
	if r.image == nil {
		r.image = canvas.NewImageFromImage(goImg)
		r.image.FillMode = canvas.ImageFillStretch
		r.objects = []fyne.CanvasObject{r.image}
	} else {
		r.image.Image = goImg
		r.image.Refresh()
	}
	r.image.Resize(size)
}
