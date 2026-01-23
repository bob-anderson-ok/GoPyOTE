package main

import (
	"bufio"
	"bytes"
	"fmt"
	"image/color"
	"image/png"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
	"gonum.org/v1/plot/vg/vgimg"
)

// Windows API
var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	procGetWindowRect       = user32.NewProc("GetWindowRect")
	procSetWindowPos        = user32.NewProc("SetWindowPos")
)

type winRect struct {
	Left, Top, Right, Bottom int32
}

const swpNoZOrder = 0x0004

func getForegroundWindow() uintptr {
	hwnd, _, _ := procGetForegroundWindow.Call()
	return hwnd
}

func getWindowRect(hwnd uintptr) (x, y, w, h int32, ok bool) {
	var rect winRect
	ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	if ret == 0 {
		return 0, 0, 0, 0, false
	}
	return rect.Left, rect.Top, rect.Right - rect.Left, rect.Bottom - rect.Top, true
}

func setWindowPos(hwnd uintptr, x, y, w, h int32) bool {
	ret, _, _ := procSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), uintptr(w), uintptr(h), swpNoZOrder)
	return ret != 0
}

// PlotPoint represents a data point in the light curve
type PlotPoint struct {
	X      float64 // Time or frame number
	Y      float64 // Brightness/magnitude
	Index  int     // Point index for identification
	Series int     // Which series this point belongs to
}

// PlotSeries represents a single light curve series
type PlotSeries struct {
	Points []PlotPoint
	Color  color.RGBA
	Name   string
}

// LightCurvePlot is a custom widget for displaying interactive light curve plots using gonum/plot
type LightCurvePlot struct {
	widget.BaseWidget
	series         []PlotSeries
	pointRadius    float32
	onPointClicked func(point PlotPoint)
	selectedSeries int
	selectedIndex  int

	// Plot bounds for coordinate conversion
	minX, maxX, minY, maxY float64
	// Plot area margins (in pixels) - approximate values for gonum/plot
	marginLeft, marginRight, marginTop, marginBottom float32
}

// NewLightCurvePlot creates a new light curve plot widget
func NewLightCurvePlot(series []PlotSeries, onPointClicked func(PlotPoint)) *LightCurvePlot {
	p := &LightCurvePlot{
		series:         series,
		pointRadius:    5,
		onPointClicked: onPointClicked,
		selectedSeries: -1,
		selectedIndex:  -1,
		marginLeft:     60,
		marginRight:    20,
		marginTop:      20,
		marginBottom:   40,
	}
	p.calculateBounds()
	p.ExtendBaseWidget(p)
	return p
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
}

// SetSeries updates the plot data
func (p *LightCurvePlot) SetSeries(series []PlotSeries) {
	p.series = series
	p.selectedSeries = -1
	p.selectedIndex = -1
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

// screenToData converts screen coordinates to data coordinates
func (p *LightCurvePlot) screenToData(pos fyne.Position, size fyne.Size) (float64, float64) {
	plotWidth := size.Width - p.marginLeft - p.marginRight
	plotHeight := size.Height - p.marginTop - p.marginBottom

	// Convert screen position to data coordinates
	dataX := p.minX + float64((pos.X-p.marginLeft)/plotWidth)*(p.maxX-p.minX)
	dataY := p.maxY - float64((pos.Y-p.marginTop)/plotHeight)*(p.maxY-p.minY)

	return dataX, dataY
}

func (p *LightCurvePlot) handleClick(pos fyne.Position) {
	if len(p.series) == 0 {
		return
	}

	size := p.Size()
	clickX, clickY := p.screenToData(pos, size)

	// Find the closest point in data space
	clickRadius := (p.maxX - p.minX) * 0.03 // 3% of data range
	var closestSeries = -1
	var closestIdx = -1
	var closestDist = clickRadius * 2

	for s, series := range p.series {
		for i, pt := range series.Points {
			dx := pt.X - clickX
			dy := (pt.Y - clickY) * (p.maxX - p.minX) / (p.maxY - p.minY) // Normalize
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist < clickRadius && dist < closestDist {
				closestDist = dist
				closestSeries = s
				closestIdx = i
			}
		}
	}

	if closestSeries >= 0 && closestIdx >= 0 {
		p.selectedSeries = closestSeries
		p.selectedIndex = closestIdx
		p.Refresh()
		if p.onPointClicked != nil {
			p.onPointClicked(p.series[closestSeries].Points[closestIdx])
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

	if size.Width < 10 || size.Height < 10 || len(p.series) == 0 {
		return
	}

	// Create gonum plot
	plt := plot.New()
	plt.Title.Text = "Light Curve"
	plt.Title.TextStyle.Font.Weight = 2 // Bold
	plt.X.Label.Text = "Time"
	plt.X.Label.TextStyle.Font.Weight = 2
	plt.Y.Label.Text = "Brightness"
	plt.Y.Label.TextStyle.Font.Weight = 2

	// Set axis ranges
	plt.X.Min = p.minX
	plt.X.Max = p.maxX
	plt.Y.Min = p.minY
	plt.Y.Max = p.maxY

	// Add grid
	plt.Add(plotter.NewGrid())

	// Add each series
	for s, series := range p.series {
		// Create XY data for line and scatter
		pts := make(plotter.XYs, len(series.Points))
		for i, pt := range series.Points {
			pts[i].X = pt.X
			pts[i].Y = pt.Y
		}

		// Create line
		line, _ := plotter.NewLine(pts)
		line.Color = series.Color
		line.Width = vg.Points(2)
		plt.Add(line)

		// Create scatter points
		scatter, _ := plotter.NewScatter(pts)
		scatter.Color = series.Color
		scatter.GlyphStyle.Shape = draw.CircleGlyph{}
		scatter.GlyphStyle.Radius = vg.Points(4)

		// Highlight the selected point
		if s == p.selectedSeries && p.selectedIndex >= 0 && p.selectedIndex < len(series.Points) {
			// Draw regular points first
			plt.Add(scatter)

			// Draw the selected point larger and in red
			selectedPt := make(plotter.XYs, 1)
			selectedPt[0].X = series.Points[p.selectedIndex].X
			selectedPt[0].Y = series.Points[p.selectedIndex].Y
			selectedScatter, _ := plotter.NewScatter(selectedPt)
			selectedScatter.Color = color.RGBA{R: 255, G: 50, B: 50, A: 255}
			selectedScatter.GlyphStyle.Shape = draw.CircleGlyph{}
			selectedScatter.GlyphStyle.Radius = vg.Points(7)
			plt.Add(selectedScatter)
		} else {
			plt.Add(scatter)
		}

		// Add to legend
		plt.Legend.Add(series.Name, line, scatter)
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
		fmt.Printf("Error encoding plot image: %v", err)
		return
	}
	goImg, _ := png.Decode(bytes.NewReader(buf.Bytes()))

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

// renderPlotToImage renders the gonum plot to an image.Image
//func renderPlotToImage(plt *plot.Plot, width, height int) image.Image {
//	w := vg.Length(width) * vg.Inch / 96
//	h := vg.Length(height) * vg.Inch / 96
//
//	img := vgimg.New(w, h)
//	dc := draw.New(img)
//	plt.Draw(dc)
//
//	return img.Image()
//}

// generateRandomLightCurve creates sample light curve data
func generateRandomLightCurve(numPoints int, seriesIndex int, baseY float64, dipCenter float64) []PlotPoint {
	points := make([]PlotPoint, numPoints)

	for i := 0; i < numPoints; i++ {
		x := float64(i)
		y := baseY + rand.Float64()*0.1 - 0.05
		dipPos := int(float64(numPoints) * dipCenter)
		if i > dipPos-numPoints/6 && i < dipPos+numPoints/6 {
			dip := 0.3 * math.Exp(-math.Pow(float64(i-dipPos)/5.0, 2))
			y -= dip
		}
		points[i] = PlotPoint{X: x, Y: y, Index: i, Series: seriesIndex}
	}
	return points
}

func main() {
	a := app.NewWithID("com.gopyote.app")
	w := a.NewWindow("My Application")

	// Load saved window geometry
	prefs := a.Preferences()
	savedX := int32(prefs.IntWithFallback("windowX", -1))
	savedY := int32(prefs.IntWithFallback("windowY", -1))
	savedW := int32(prefs.IntWithFallback("windowW", 1000))
	savedH := int32(prefs.IntWithFallback("windowH", 600))
	firstRun := savedX == -1 && savedY == -1

	// Create a menu
	fileMenu := fyne.NewMenu("File",
		fyne.NewMenuItem("New", func() {}),
		fyne.NewMenuItem("Open", func() {}),
		fyne.NewMenuItem("Save", func() {}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Quit", func() { a.Quit() }),
	)
	editMenu := fyne.NewMenu("Edit",
		fyne.NewMenuItem("Cut", func() {}),
		fyne.NewMenuItem("Copy", func() {}),
		fyne.NewMenuItem("Paste", func() {}),
	)
	helpMenu := fyne.NewMenu("Help",
		fyne.NewMenuItem("About", func() {}),
	)
	mainMenu := fyne.NewMainMenu(fileMenu, editMenu, helpMenu)
	w.SetMainMenu(mainMenu)

	// Helper to create tab content with a colored background
	makeTabContent := func(text string, bgColor color.Color) *fyne.Container {
		bg := canvas.NewRectangle(bgColor)
		label := widget.NewLabel(text)
		return container.NewStack(bg, container.NewCenter(label))
	}

	// Tab 1: Country of origin
	tab1Bg := canvas.NewRectangle(color.RGBA{R: 200, G: 230, B: 200, A: 255})
	buttonSize := fyne.NewSize(180, 35)
	countryBtn1 := widget.NewButton("Select Country", func() {})
	countryBtn1.Resize(buttonSize)
	countryCheck1 := widget.NewCheck("Enabled", func(bool) {})
	row1 := container.NewHBox(container.New(layout.NewGridWrapLayout(buttonSize), countryBtn1), countryCheck1)

	countryBtn2 := widget.NewButton("View Details", func() {})
	countryBtn2.Resize(buttonSize)
	countryCheck2 := widget.NewCheck("Detailed", func(bool) {})
	row2 := container.NewHBox(container.New(layout.NewGridWrapLayout(buttonSize), countryBtn2), countryCheck2)

	countryBtn3 := widget.NewButton("Edit Entry", func() {})
	countryBtn3.Resize(buttonSize)
	countryCheck3 := widget.NewCheck("Editable", func(bool) {})
	row3 := container.NewHBox(container.New(layout.NewGridWrapLayout(buttonSize), countryBtn3), countryCheck3)

	countryBtn4 := widget.NewButton("Delete Entry", func() {})
	countryBtn4.Resize(buttonSize)
	countryCheck4 := widget.NewCheck("Confirm", func(bool) {})
	row4 := container.NewHBox(container.New(layout.NewGridWrapLayout(buttonSize), countryBtn4), countryCheck4)

	countryButtons := container.NewVBox(row1, row2, row3, row4)
	verticallyCentered := container.NewVBox(
		layout.NewSpacer(),
		countryButtons,
		layout.NewSpacer(),
	)
	leftAlignedButtons := container.NewBorder(nil, nil, verticallyCentered, nil, nil)
	tab1Content := container.NewStack(tab1Bg, container.NewPadded(leftAlignedButtons))
	tab1 := container.NewTabItem("Country of origin", tab1Content)

	// Tab 2: Settings
	tab2 := container.NewTabItem("Settings",
		makeTabContent("Settings page content", color.RGBA{R: 200, G: 200, B: 230, A: 255}))

	// Tab 3: Data
	tab3Bg := canvas.NewRectangle(color.RGBA{R: 230, G: 220, B: 200, A: 255})
	imagePlaceholder := canvas.NewRectangle(color.RGBA{R: 200, G: 200, B: 200, A: 255})
	imagePlaceholder.SetMinSize(fyne.NewSize(300, 200))
	imageLabel := widget.NewLabel("Image Placeholder")
	imageHolder := container.NewStack(imagePlaceholder, container.NewCenter(imageLabel))
	tab3Content := container.NewStack(tab3Bg, container.NewPadded(container.NewVBox(
		widget.NewLabel("Data page content"),
		imageHolder,
	)))
	tab3 := container.NewTabItem("Data", tab3Content)

	// Tab 4: Reports
	tab4Bg := canvas.NewRectangle(color.RGBA{R: 230, G: 200, B: 220, A: 255})
	radioGroup := widget.NewRadioGroup([]string{
		"Option 1", "Option 2", "Option 3", "Option 4", "Option 5", "Option 6",
	}, func(selected string) {})
	radioGroup.SetSelected("Option 1")
	tab4Content := container.NewStack(tab4Bg, container.NewPadded(container.NewVBox(
		widget.NewLabel("Reports page content"),
		radioGroup,
	)))
	tab4 := container.NewTabItem("Reports", tab4Content)
	tabs := container.NewAppTabs(tab1, tab2, tab3, tab4)

	// Create the plot area with an interactive light curve
	plotStatusLabel := widget.NewLabel("Click on a point to see details")
	plotStatusLabel.Wrapping = fyne.TextWrapWord

	// Generate two light curve series with different colors
	series1Color := color.RGBA{R: 70, G: 130, B: 180, A: 255} // Steel blue
	series2Color := color.RGBA{R: 220, G: 120, B: 50, A: 255} // Orange

	lightCurveSeries := []PlotSeries{
		{
			Points: generateRandomLightCurve(50, 0, 1.0, 0.4),
			Color:  series1Color,
			Name:   "Star A",
		},
		{
			Points: generateRandomLightCurve(50, 1, 0.8, 0.6),
			Color:  series2Color,
			Name:   "Star B",
		},
	}

	// Create the plot with a click callback
	lightCurvePlot := NewLightCurvePlot(lightCurveSeries, func(point PlotPoint) {
		seriesName := lightCurveSeries[point.Series].Name
		plotStatusLabel.SetText(fmt.Sprintf("%s - Point %d\nTime: %.2f\nBrightness: %.4f",
			seriesName, point.Index, point.X, point.Y))
	})

	plotArea := container.NewBorder(
		nil,             // top
		plotStatusLabel, // bottom
		nil,             // left
		nil,             // right
		lightCurvePlot,  // center
	)

	// Create buttons
	btn1 := widget.NewButton("Action 1", func() {})
	btn2 := widget.NewButton("Action 2", func() {})
	btn3 := widget.NewButton("Action 3", func() {})
	btn4 := widget.NewButton("Regenerate Plot", func() {
		newSeries := []PlotSeries{
			{
				Points: generateRandomLightCurve(50, 0, 1.0, 0.3+rand.Float64()*0.4),
				Color:  series1Color,
				Name:   "Star A",
			},
			{
				Points: generateRandomLightCurve(50, 1, 0.8, 0.3+rand.Float64()*0.4),
				Color:  series2Color,
				Name:   "Star B",
			},
		}
		lightCurvePlot.SetSeries(newSeries)
		plotStatusLabel.SetText("Plot regenerated - click a point")
	})
	btnIOTA := widget.NewButton("Run IOTAdiffraction", func() {
		// Get the current working directory
		cwd, err := os.Getwd()
		if err != nil {
			dialog.ShowError(fmt.Errorf("could not determine current directory: %v", err), w)
			return
		}

		// Build the path to IOTAdiffraction.exe
		exePath := filepath.Join(cwd, "IOTAdiffraction.exe")

		// Check if the file exists
		if _, err := os.Stat(exePath); os.IsNotExist(err) {
			dialog.ShowInformation("File Not Found",
				"IOTAdiffraction.exe was not found in the current directory.\n\n"+
					"Please ensure the file is located at:\n"+exePath, w)
			return
		}

		// Create output display
		outputLabel := widget.NewLabel("")
		outputLabel.Wrapping = fyne.TextWrapWord

		scrollContainer := container.NewVScroll(container.NewPadded(outputLabel))
		scrollContainer.SetMinSize(fyne.NewSize(500, 300))

		// Create a custom dialog with the output
		outputDialog := dialog.NewCustom("IOTAdiffraction Output", "Close", scrollContainer, w)

		// Set up the command with pipes
		cmd := exec.Command(exePath, "parameters")
		cmd.Dir = cwd

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			dialog.ShowError(fmt.Errorf("error creating stdout pipe: %v", err), w)
			return
		}

		stderr, err := cmd.StderrPipe()
		if err != nil {
			dialog.ShowError(fmt.Errorf("error creating stderr pipe: %v", err), w)
			return
		}

		// Start the command
		if err := cmd.Start(); err != nil {
			dialog.ShowError(fmt.Errorf("error starting IOTAdiffraction: %v", err), w)
			return
		}

		outputDialog.Show()

		// Mutex to protect output text updates
		var mu sync.Mutex
		var outputLines string

		appendOutput := func(line string) {
			mu.Lock()
			outputLines += line + "\n"
			text := outputLines
			mu.Unlock()
			fyne.Do(func() {
				outputLabel.SetText(text)
				scrollContainer.ScrollToBottom()
			})
		}

		// Read stdout in a goroutine
		go func() {
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				appendOutput(scanner.Text())
			}
		}()

		// Read stderr in a goroutine
		go func() {
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				appendOutput("[stderr] " + scanner.Text())
			}
		}()

		// Wait for completion in a goroutine
		go func() {
			err := cmd.Wait()
			if err != nil {
				appendOutput(fmt.Sprintf("\n[Error: %v]", err))
			} else {
				appendOutput("\n[Process completed successfully]")
			}
		}()
	})
	buttons := container.NewHBox(btn1, btn2, btn3, btn4, btnIOTA)

	// Split tabs and plot area
	split := container.NewHSplit(tabs, plotArea)
	splitOffset := prefs.FloatWithFallback("splitOffset", 0.6)
	split.SetOffset(splitOffset)

	content := container.NewBorder(nil, buttons, nil, nil, split)

	w.SetContent(content)
	w.Resize(fyne.NewSize(float32(savedW), float32(savedH)))

	if firstRun {
		w.CenterOnScreen()
	}

	// Save window geometry and split position on close
	w.SetCloseIntercept(func() {
		prefs.SetFloat("splitOffset", split.Offset)
		if hwnd := getForegroundWindow(); hwnd != 0 {
			if x, y, width, height, ok := getWindowRect(hwnd); ok {
				prefs.SetInt("windowX", int(x))
				prefs.SetInt("windowY", int(y))
				prefs.SetInt("windowW", int(width))
				prefs.SetInt("windowH", int(height))
			}
		}
		w.Close()
	})

	w.Show()

	if !firstRun {
		go func() {
			time.Sleep(100 * time.Millisecond)
			if hwnd := getForegroundWindow(); hwnd != 0 {
				setWindowPos(hwnd, savedX, savedY, savedW, savedH)
			}
		}()
	}

	a.Run()
}
