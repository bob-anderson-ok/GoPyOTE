// Package lightcurve provides functions for extracting light curves from diffraction images,
// plotting observation lines on display images, and detecting edges in geometric shadow images.
package lightcurve

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"

	"gonum.org/v1/plot"
	_ "gonum.org/v1/plot/font/liberation"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	vgdraw "gonum.org/v1/plot/vg/draw"
	"gonum.org/v1/plot/vg/vgimg"
)

// PathPoint represents a point along the observation path with x, y coordinates
// and distance from the start of the path.
type PathPoint struct {
	X                 float64 // X coordinate in image pixels
	Y                 float64 // Y coordinate in image pixels
	DistanceFromStart float64 // Distance from path start in pixels
}

// Point represents a single point on the extracted light curve.
type Point struct {
	Distance  float64 // Distance from path start (in km or pixels depending on scale)
	Intensity float64 // Normalized intensity value
}

// ObservationPath defines the path along which the light curve is extracted.
type ObservationPath struct {
	// Input parameters (from parameter file)
	DxKmPerSec             float64 // Shadow velocity X component (km/sec)
	DyKmPerSec             float64 // Shadow velocity Y component (km/sec)
	PathOffsetFromCenterKm float64 // Perpendicular offset from the center (km), positive = right from star's perspective

	FundamentalPlaneWidthKm  float64 // Width of the fundamental plane in km
	FundamentalPlaneWidthPts int     // Width of the fundamental plane in pixels

	// Computed values
	StartX              float64     // Starting X coordinate in pixels
	StartY              float64     // Starting Y coordinate in pixels
	EndX                float64     // Ending X coordinate in pixels
	EndY                float64     // Ending Y coordinate in pixels
	ShadowSpeedKmPerSec float64     // Shadow speed (computed from Dx and Dy)
	PathAngleDegrees    float64     // Path angle in degrees
	Direction           string      // Path direction description
	SamplePoints        []PathPoint // Computed sample points along the path
}

// annotatedPoint is used internally for path intersection calculations.
type annotatedPoint struct {
	X, Y     float64
	Position string // "top", "bottom", "left", or "right"
}

// ErrNoIntersection is returned when the path does not intersect the image boundaries.
var ErrNoIntersection = errors.New("line does not intersect square")

// ComputePathFromVelocity computes the observation path start and end points
// from the velocity components (DxKmPerSec, DyKmPerSec) and path offset.
// This matches the calculation used in the main IOTAdiffraction application.
func (p *ObservationPath) ComputePathFromVelocity() error {
	// Compute shadow speed
	p.ShadowSpeedKmPerSec = math.Sqrt(p.DxKmPerSec*p.DxKmPerSec + p.DyKmPerSec*p.DyKmPerSec)

	if p.ShadowSpeedKmPerSec == 0 {
		return errors.New("shadow speed is zero (both Dx and Dy are zero)")
	}

	// Compute the path angle (measured CCW from y-axis)
	p.PathAngleDegrees = math.Atan2(-p.DxKmPerSec, -p.DyKmPerSec) * 180.0 / math.Pi
	if p.PathAngleDegrees < 0.0 {
		p.PathAngleDegrees += 360.0
	}

	Npts := p.FundamentalPlaneWidthPts
	w := float64(Npts - 1)
	theta := p.PathAngleDegrees * math.Pi / 180.0

	// Convert offset from km to pixels
	d := (p.PathOffsetFromCenterKm / p.FundamentalPlaneWidthKm) * float64(Npts)

	// Find where the path intersects the image boundaries
	p1, p2, dx, dy, err := pathSquareIntersections(w, theta, d)
	if err != nil {
		return fmt.Errorf("path does not intersect image: %w", err)
	}

	// Move origin from center to the upper-left corner
	delta := float64(Npts) / 2.0
	p1.X += delta
	p1.Y += delta
	p2.X += delta
	p2.Y += delta

	// Determine the direction and set start/end points
	useTopBottomLogic := (p1.Position == "top" || p1.Position == "bottom") &&
		(p2.Position == "top" || p2.Position == "bottom")

	if useTopBottomLogic {
		if dy < 0 {
			p.Direction = "top to bottom"
			if p1.Position == "top" {
				p.setStartEnd(p1, p2)
			} else {
				p.setStartEnd(p2, p1)
			}
		} else {
			p.Direction = "bottom to top"
			if p1.Position == "bottom" {
				p.setStartEnd(p1, p2)
			} else {
				p.setStartEnd(p2, p1)
			}
		}
	} else {
		if dx < 0 {
			p.Direction = "left to right"
			if p1.Position == "left" {
				p.setStartEnd(p1, p2)
			} else {
				p.setStartEnd(p2, p1)
			}
		} else {
			p.Direction = "right to left"
			if p1.Position == "right" {
				p.setStartEnd(p1, p2)
			} else {
				p.setStartEnd(p2, p1)
			}
		}
	}

	return nil
}

func (p *ObservationPath) setStartEnd(pStart, pEnd annotatedPoint) {
	p.StartX = pStart.X
	p.StartY = pStart.Y
	p.EndX = pEnd.X
	p.EndY = pEnd.Y
}

// pathSquareIntersections finds where a line intersects a square centered at origin.
// w: square width
// theta: angle of line measured CCW from y-axis (radians)
// d: perpendicular distance from origin to the line
// Returns the two intersection points, direction vector (dx, dy), and error.
func pathSquareIntersections(w, theta, d float64) (annotatedPoint, annotatedPoint, float64, float64, error) {
	halfW := w / 2.0

	// Direction vector of the line (perpendicular to the normal)
	dx := math.Sin(theta)
	dy := math.Cos(theta)

	// Normal vector pointing in the direction of offset (perpendicular to line, rotated 90° CW)
	nx := dy  // cos(theta)
	ny := -dx // -sin(theta)

	// A point on the line: offset from origin by distance d along the normal
	x0 := d * nx
	y0 := d * ny

	// Line parametric form: x = x0 + t*dx, y = y0 + t*dy
	// Find intersections with the four sides of the square
	var intersections []annotatedPoint

	// Right edge: x = halfW
	if math.Abs(dx) > 1e-12 {
		t := (halfW - x0) / dx
		y := y0 + t*dy
		if y >= -halfW && y <= halfW {
			intersections = append(intersections, annotatedPoint{halfW, y, "right"})
		}
	}

	// Left edge: x = -halfW
	if math.Abs(dx) > 1e-12 {
		t := (-halfW - x0) / dx
		y := y0 + t*dy
		if y >= -halfW && y <= halfW {
			intersections = append(intersections, annotatedPoint{-halfW, y, "left"})
		}
	}

	// Bottom edge: y = halfW
	if math.Abs(dy) > 1e-12 {
		t := (halfW - y0) / dy
		x := x0 + t*dx
		if x >= -halfW && x <= halfW {
			intersections = append(intersections, annotatedPoint{x, halfW, "bottom"})
		}
	}

	// Top edge: y = -halfW
	if math.Abs(dy) > 1e-12 {
		t := (-halfW - y0) / dy
		x := x0 + t*dx
		if x >= -halfW && x <= halfW {
			intersections = append(intersections, annotatedPoint{x, -halfW, "top"})
		}
	}

	// Remove duplicate corner intersections
	intersections = removeDuplicatePoints(intersections, 1e-9)

	if len(intersections) < 2 {
		return annotatedPoint{}, annotatedPoint{}, dx, dy, ErrNoIntersection
	}
	return intersections[0], intersections[1], dx, dy, nil
}

func removeDuplicatePoints(pts []annotatedPoint, tol float64) []annotatedPoint {
	var result []annotatedPoint
	for _, p := range pts {
		duplicate := false
		for _, r := range result {
			if math.Abs(p.X-r.X) < tol && math.Abs(p.Y-r.Y) < tol {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, p)
		}
	}
	return result
}

// ComputeSamplePoints generates sample points along the observation path.
// Points are sampled at 1-pixel intervals along the path.
func (p *ObservationPath) ComputeSamplePoints() {
	xLength := p.EndX - p.StartX
	yLength := p.EndY - p.StartY
	pathLength := math.Sqrt(xLength*xLength + yLength*yLength)

	dYPerStep := yLength / pathLength
	dXPerStep := xLength / pathLength

	p.SamplePoints = nil
	for i := 0; i < int(math.Round(pathLength)); i++ {
		k := float64(i)
		xVal := p.StartX + k*dXPerStep
		yVal := p.StartY + k*dYPerStep
		distanceFromStart := math.Sqrt(k*k*dXPerStep*dXPerStep + k*k*dYPerStep*dYPerStep)
		p.SamplePoints = append(p.SamplePoints, PathPoint{
			X:                 xVal,
			Y:                 yVal,
			DistanceFromStart: distanceFromStart,
		})
	}
}

// interpolate performs bilinear interpolation on a 2D matrix at the given (x, y) coordinates.
func interpolate(matrix [][]float64, x, y float64) float64 {
	n := len(matrix)
	if n == 0 {
		return 0
	}

	// Clamp to valid range
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	if x >= float64(n-1) {
		x = float64(n-1) - 1e-9
	}
	if y >= float64(n-1) {
		y = float64(n-1) - 1e-9
	}

	// Integer indices
	x0 := int(x)
	y0 := int(y)
	x1 := x0 + 1
	y1 := y0 + 1

	// Fractional parts
	xFrac := x - float64(x0)
	yFrac := y - float64(y0)

	// Four surrounding values
	v00 := matrix[y0][x0]
	v01 := matrix[y0][x1]
	v10 := matrix[y1][x0]
	v11 := matrix[y1][x1]

	// Bilinear interpolation
	v0 := v00*(1-xFrac) + v01*xFrac
	v1 := v10*(1-xFrac) + v11*xFrac

	return v0*(1-yFrac) + v1*yFrac
}

// LoadGray16PNG loads a 16-bit grayscale PNG image and returns it as a 2D float64 matrix.
// The scale parameter is used to convert pixel values back to intensity: intensity = pixelValue / scale.
func LoadGray16PNG(filename string, scale float64) (matrix [][]float64, err error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", filename, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("failed to decode %s: %w", filename, err)
	}

	bounds := img.Bounds()
	h := bounds.Dy()
	w := bounds.Dx()

	matrix = make([][]float64, h)
	for y := 0; y < h; y++ {
		matrix[y] = make([]float64, w)
		for x := 0; x < w; x++ {
			c := img.At(x+bounds.Min.X, y+bounds.Min.Y)
			gray, ok := c.(color.Gray16)
			if !ok {
				// Try to convert
				r, g, b, _ := c.RGBA()
				grayVal := (r + g + b) / 3
				matrix[y][x] = float64(grayVal) / scale
			} else {
				matrix[y][x] = float64(gray.Y) / scale
			}
		}
	}

	return matrix, nil
}

// LoadGray8PNG loads an 8-bit grayscale PNG image and returns it as a 2D float64 matrix.
// Values are normalized to the [0, 1] range.
func LoadGray8PNG(filename string) (matrix [][]float64, err error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", filename, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("failed to decode %s: %w", filename, err)
	}

	bounds := img.Bounds()
	h := bounds.Dy()
	w := bounds.Dx()

	matrix = make([][]float64, h)
	for y := 0; y < h; y++ {
		matrix[y] = make([]float64, w)
		for x := 0; x < w; x++ {
			c := img.At(x+bounds.Min.X, y+bounds.Min.Y)
			r, g, b, _ := c.RGBA()
			grayVal := (r + g + b) / 3 / 256 // Convert to 8-bit range
			if grayVal > 0 {
				matrix[y][x] = 1.0
			} else {
				matrix[y][x] = 0.0
			}
		}
	}

	return matrix, nil
}

// ExtractLightCurve extracts intensity values along the observation path from the intensity matrix.
// Returns a slice of LightCurvePoints with distance and intensity values.
func ExtractLightCurve(intensityMatrix [][]float64, path *ObservationPath) []Point {
	if len(path.SamplePoints) == 0 {
		path.ComputeSamplePoints()
	}

	distancePerPoint := path.FundamentalPlaneWidthKm / float64(path.FundamentalPlaneWidthPts)

	lightCurve := make([]Point, len(path.SamplePoints))
	for i, pt := range path.SamplePoints {
		intensity := interpolate(intensityMatrix, pt.X, pt.Y)
		lightCurve[i] = Point{
			Distance:  pt.DistanceFromStart * distancePerPoint,
			Intensity: intensity,
		}
	}

	return lightCurve
}

// FindEdgesInGeometricShadow detects edge transitions in the geometric shadow image
// along the observation path. Returns the distances (from path start) where edges occur.
// An edge is detected when the interpolated value transitions between 0 and 1.
func FindEdgesInGeometricShadow(geometricMatrix [][]float64, path *ObservationPath) []float64 {
	if len(path.SamplePoints) == 0 {
		path.ComputeSamplePoints()
	}

	var edges []float64
	colorAtNextEdge := 1.0

	for _, pt := range path.SamplePoints {
		pixelValue := interpolate(geometricMatrix, pt.X, pt.Y)
		if pixelValue > 0.0 {
			pixelValue = 1.0
		}
		if pixelValue == colorAtNextEdge {
			edges = append(edges, pt.DistanceFromStart)
			colorAtNextEdge = 1.0 - colorAtNextEdge // Toggle
		}
	}

	return edges
}

// StepTicks is a custom tick marker for plots with fixed step intervals.
type StepTicks struct {
	Step   float64
	Format string
}

func (t StepTicks) Ticks(min, max float64) []plot.Tick {
	var ticks []plot.Tick
	start := math.Ceil(min/t.Step) * t.Step
	for v := start; v <= max; v += t.Step {
		ticks = append(ticks, plot.Tick{
			Value: v,
			Label: fmt.Sprintf(t.Format, v),
		})
	}
	return ticks
}

// PlotLightCurve creates a plot of the light curve with optional edge markers.
// Returns the plot as an image.Image.
func PlotLightCurve(lightCurve []Point, edges []float64, path *ObservationPath, wPx, hPx float64) (image.Image, error) {
	p := plot.New()

	p.Y.Min = -0.2
	p.Y.Max = 1.5

	// Font settings
	p.Title.TextStyle.Font.Typeface = "Liberation"
	p.Title.TextStyle.Font.Variant = "Sans"
	p.Title.TextStyle.Font.Size = vg.Points(12)

	p.X.Label.TextStyle.Font.Typeface = "Liberation"
	p.X.Label.TextStyle.Font.Variant = "Sans"
	p.X.Label.TextStyle.Font.Size = vg.Points(12)

	p.Y.Label.TextStyle.Font.Typeface = "Liberation"
	p.Y.Label.TextStyle.Font.Variant = "Sans"
	p.Y.Label.TextStyle.Font.Size = vg.Points(12)

	p.X.Tick.Label.Font.Typeface = "Liberation"
	p.X.Tick.Label.Font.Variant = "Sans"
	p.X.Tick.Label.Font.Size = vg.Points(10)

	p.Y.Tick.Label.Font.Typeface = "Liberation"
	p.Y.Tick.Label.Font.Variant = "Sans"
	p.Y.Tick.Label.Font.Size = vg.Points(10)

	distancePerPoint := path.FundamentalPlaneWidthKm / float64(path.FundamentalPlaneWidthPts)
	pointSpan := path.SamplePoints[len(path.SamplePoints)-1].DistanceFromStart

	p.Title.Text = "Light curve along observation path"
	p.X.Label.Text = fmt.Sprintf("km (divide by shadow speed of %.3f km/s for time)", path.ShadowSpeedKmPerSec)
	p.Y.Label.Text = "normalized intensity"
	p.X.Tick.Marker = StepTicks{Step: pointSpan * distancePerPoint / 20, Format: "%.2f"}
	p.Y.Tick.Marker = StepTicks{Step: 0.2, Format: "%.2f"}
	p.Add(plotter.NewGrid())

	// Plot the light curve data
	n := len(lightCurve)
	pts := make(plotter.XYs, n)
	for i := 0; i < n; i++ {
		pts[i].X = lightCurve[i].Distance
		pts[i].Y = lightCurve[i].Intensity
	}

	line, err := plotter.NewLine(pts)
	if err != nil {
		return nil, err
	}
	line.Color = color.RGBA{R: 0, G: 0, B: 255, A: 255}
	p.Add(line)

	// Add edge markers as red dashed vertical lines
	for _, edge := range edges {
		vpts := plotter.XYs{
			{X: edge * distancePerPoint, Y: -0.1},
			{X: edge * distancePerPoint, Y: 1.3},
		}

		vline, err := plotter.NewLine(vpts)
		if err != nil {
			return nil, err
		}
		vline.Dashes = []vg.Length{vg.Points(6), vg.Points(4)}
		vline.Color = color.RGBA{R: 255, G: 0, B: 0, A: 255}
		p.Add(vline)
	}

	// Add a zero line
	hpts := plotter.XYs{
		{X: 0.0, Y: 0.0},
		{X: pointSpan * distancePerPoint, Y: 0.0},
	}
	hline, err := plotter.NewLine(hpts)
	if err != nil {
		return nil, err
	}
	hline.Dashes = []vg.Length{vg.Points(6), vg.Points(4)}
	hline.Color = color.RGBA{R: 0, G: 0, B: 0, A: 255}
	p.Add(hline)

	// Render to image
	const dpi = 96
	width := vg.Length(wPx) * vg.Inch / dpi
	height := vg.Length(hPx) * vg.Inch / dpi

	c := vgimg.New(width, height)
	dc := vgdraw.New(c)
	p.Draw(dc)

	return c.Image(), nil
}

// SaveLightCurvePlot creates and saves a light curve plot to a PNG file.
func SaveLightCurvePlot(filename string, lightCurve []Point, edges []float64, path *ObservationPath, wPx, hPx float64) (err error) {
	img, err := PlotLightCurve(lightCurve, edges, path, wPx, hPx)
	if err != nil {
		return err
	}

	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	return png.Encode(f, img)
}

// DrawObservationLineOnImage draws the observation path on an 8-bit image.
// The path is drawn as a red line with a red dot at the start and a green dot at the end.
// Returns a new RGBA image with the line drawn on it.
func DrawObservationLineOnImage(sourceImage image.Image, path *ObservationPath) (*image.RGBA, error) {
	bounds := sourceImage.Bounds()

	// Create a new RGBA image to draw on
	result := image.NewRGBA(bounds)
	draw.Draw(result, bounds, sourceImage, bounds.Min, draw.Src)

	// Draw the observation line
	drawLine(result, path.StartX, path.StartY, path.EndX, path.EndY, color.RGBA{R: 255, A: 255})

	// Draw the start dot (red)
	drawDot(result, path.StartX, path.StartY, 5, color.RGBA{R: 255, A: 255})

	// Draw the end dot (green)
	drawDot(result, path.EndX, path.EndY, 5, color.RGBA{G: 255, A: 255})

	return result, nil
}

// drawLine draws a line on the image using Bresenham's algorithm.
func drawLine(img *image.RGBA, x1, y1, x2, y2 float64, col color.Color) {
	dx := math.Abs(x2 - x1)
	dy := math.Abs(y2 - y1)
	sx := -1.0
	if x1 < x2 {
		sx = 1.0
	}
	sy := -1.0
	if y1 < y2 {
		sy = 1.0
	}
	err := dx - dy

	for {
		// Draw a thick line (3 pixels wide)
		for oy := -1; oy <= 1; oy++ {
			for ox := -1; ox <= 1; ox++ {
				px := int(x1) + ox
				py := int(y1) + oy
				if px >= 0 && px < img.Bounds().Dx() && py >= 0 && py < img.Bounds().Dy() {
					img.Set(px, py, col)
				}
			}
		}

		if math.Abs(x1-x2) < 1 && math.Abs(y1-y2) < 1 {
			break
		}
		e2 := 2 * err
		if e2 > -dy {
			err -= dy
			x1 += sx
		}
		if e2 < dx {
			err += dx
			y1 += sy
		}
	}
}

// drawDot draws a filled circle on the image.
func drawDot(img *image.RGBA, cx, cy float64, radius int, col color.Color) {
	for y := -radius; y <= radius; y++ {
		for x := -radius; x <= radius; x++ {
			if x*x+y*y <= radius*radius {
				px := int(cx) + x
				py := int(cy) + y
				if px >= 0 && px < img.Bounds().Dx() && py >= 0 && py < img.Bounds().Dy() {
					img.Set(px, py, col)
				}
			}
		}
	}
}

// LoadImageFromFile loads any PNG image file.
func LoadImageFromFile(filename string) (img image.Image, err error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", filename, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	img, err = png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("failed to decode %s: %w", filename, err)
	}

	return img, nil
}

// SaveImageToFile saves an image to a PNG file.
func SaveImageToFile(filename string, img image.Image) (err error) {
	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", filename, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	return png.Encode(f, img)
}
