package main

import (
	"bufio"
	"bytes"
	"fmt"
	"image/color"
	"image/png"
	"io"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

	"github.com/KevinWang15/go-json5"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
	"gonum.org/v1/plot/vg/vgimg"
)

// Version information
const Version = "1.0.2"

// Track the last loaded parameters file path for use by Run IOTAdiffraction
var lastLoadedParamsPath string

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

// EllipseParams represents ellipse parameters for the main body or satellite
type EllipseParams struct {
	XCenterKm          float64 `json:"x_center_km"`
	YCenterKm          float64 `json:"y_center_km"`
	MajorAxisKm        float64 `json:"major_axis_km"`
	MinorAxisKm        float64 `json:"minor_axis_km"`
	MajorAxisPaDegrees float64 `json:"major_axis_pa_degrees"`
}

// OccultationParameters holds all parameters for occultation calculations
type OccultationParameters struct {
	WindowSizePixels               int           `json:"window_size_pixels"`
	Title                          string        `json:"title"`
	FundamentalPlaneWidthKm        float64       `json:"fundamental_plane_width_km"`
	FundamentalPlaneWidthNumPoints int           `json:"fundamental_plane_width_num_points"`
	ParallaxArcsec                 float64       `json:"parallax_arcsec"`
	DistanceAu                     float64       `json:"distance_au"`
	PathToQeTableFile              string        `json:"path_to_qe_table_file"`
	ObservationWavelengthNm        int           `json:"observation_wavelength_nm"`
	DXKmPerSec                     float64       `json:"dX_km_per_sec"`
	DYKmPerSec                     float64       `json:"dY_km_per_sec"`
	PathPerpendicularOffsetKm      float64       `json:"path_perpendicular_offset_from_center_km"`
	PercentMagDrop                 int           `json:"percent_mag_drop"`
	StarDiamOnPlaneMas             float64       `json:"star_diam_on_plane_mas"`
	LimbDarkeningCoeff             float64       `json:"limb_darkening_coeff"`
	StarClass                      string        `json:"star_class"`
	MainBody                       EllipseParams `json:"main_body"`
	Satellite                      EllipseParams `json:"satellite"`
	PathToExternalImage            string        `json:"path_to_external_image"`
}

// parseOccultationParameters parses a JSON5 parameters file
func parseOccultationParameters(reader io.Reader) (*OccultationParameters, error) {
	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var params OccultationParameters
	if err := json5.Unmarshal(content, &params); err != nil {
		return nil, fmt.Errorf("failed to parse parameters: %w", err)
	}

	return &params, nil
}

// showOccultationParametersDialog displays a form dialog for editing occultation parameters
func showOccultationParametersDialog(w fyne.Window) {
	// Create entry fields for all parameters
	windowSizeEntry := widget.NewEntry()
	titleEntry := widget.NewEntry()
	fundamentalPlaneWidthKmEntry := widget.NewEntry()
	fundamentalPlaneWidthNumPointsEntry := widget.NewEntry()
	parallaxArcsecEntry := widget.NewEntry()
	distanceAuEntry := widget.NewEntry()
	pathToQeTableFileEntry := widget.NewEntry()
	observationWavelengthNmEntry := widget.NewEntry()
	dXKmPerSecEntry := widget.NewEntry()
	dYKmPerSecEntry := widget.NewEntry()
	pathPerpendicularOffsetKmEntry := widget.NewEntry()
	percentMagDropEntry := widget.NewEntry()
	starDiamOnPlaneMasEntry := widget.NewEntry()
	limbDarkeningCoeffEntry := widget.NewEntry()
	starClassEntry := widget.NewEntry()

	// Main body ellipse parameters
	mainBodyXCenterEntry := widget.NewEntry()
	mainBodyYCenterEntry := widget.NewEntry()
	mainBodyMajorAxisEntry := widget.NewEntry()
	mainBodyMinorAxisEntry := widget.NewEntry()
	mainBodyPaDegreesEntry := widget.NewEntry()

	// Satellite ellipse parameters
	satelliteXCenterEntry := widget.NewEntry()
	satelliteYCenterEntry := widget.NewEntry()
	satelliteMajorAxisEntry := widget.NewEntry()
	satelliteMinorAxisEntry := widget.NewEntry()
	satellitePaDegreesEntry := widget.NewEntry()

	pathToExternalImageEntry := widget.NewEntry()

	// Track the currently loaded file name for save dialog default
	var loadedFileName string

	// Auto-load previously opened parameters file if available
	if lastLoadedParamsPath != "" {
		file, err := os.Open(lastLoadedParamsPath)
		if err == nil {
			params, parseErr := parseOccultationParameters(file)
			if closeErr := file.Close(); closeErr != nil {
				dialog.ShowError(fmt.Errorf("failed to close file: %w", closeErr), w)
			}
			if parseErr == nil {
				windowSizeEntry.SetText(strconv.Itoa(params.WindowSizePixels))
				titleEntry.SetText(params.Title)
				fundamentalPlaneWidthKmEntry.SetText(strconv.FormatFloat(params.FundamentalPlaneWidthKm, 'f', -1, 64))
				fundamentalPlaneWidthNumPointsEntry.SetText(strconv.Itoa(params.FundamentalPlaneWidthNumPoints))
				parallaxArcsecEntry.SetText(strconv.FormatFloat(params.ParallaxArcsec, 'f', -1, 64))
				distanceAuEntry.SetText(strconv.FormatFloat(params.DistanceAu, 'f', -1, 64))
				pathToQeTableFileEntry.SetText(params.PathToQeTableFile)
				observationWavelengthNmEntry.SetText(strconv.Itoa(params.ObservationWavelengthNm))
				dXKmPerSecEntry.SetText(strconv.FormatFloat(params.DXKmPerSec, 'f', -1, 64))
				dYKmPerSecEntry.SetText(strconv.FormatFloat(params.DYKmPerSec, 'f', -1, 64))
				pathPerpendicularOffsetKmEntry.SetText(strconv.FormatFloat(params.PathPerpendicularOffsetKm, 'f', -1, 64))
				percentMagDropEntry.SetText(strconv.Itoa(params.PercentMagDrop))
				starDiamOnPlaneMasEntry.SetText(strconv.FormatFloat(params.StarDiamOnPlaneMas, 'f', -1, 64))
				limbDarkeningCoeffEntry.SetText(strconv.FormatFloat(params.LimbDarkeningCoeff, 'f', -1, 64))
				starClassEntry.SetText(params.StarClass)
				mainBodyXCenterEntry.SetText(strconv.FormatFloat(params.MainBody.XCenterKm, 'f', -1, 64))
				mainBodyYCenterEntry.SetText(strconv.FormatFloat(params.MainBody.YCenterKm, 'f', -1, 64))
				mainBodyMajorAxisEntry.SetText(strconv.FormatFloat(params.MainBody.MajorAxisKm, 'f', -1, 64))
				mainBodyMinorAxisEntry.SetText(strconv.FormatFloat(params.MainBody.MinorAxisKm, 'f', -1, 64))
				mainBodyPaDegreesEntry.SetText(strconv.FormatFloat(params.MainBody.MajorAxisPaDegrees, 'f', -1, 64))
				satelliteXCenterEntry.SetText(strconv.FormatFloat(params.Satellite.XCenterKm, 'f', -1, 64))
				satelliteYCenterEntry.SetText(strconv.FormatFloat(params.Satellite.YCenterKm, 'f', -1, 64))
				satelliteMajorAxisEntry.SetText(strconv.FormatFloat(params.Satellite.MajorAxisKm, 'f', -1, 64))
				satelliteMinorAxisEntry.SetText(strconv.FormatFloat(params.Satellite.MinorAxisKm, 'f', -1, 64))
				satellitePaDegreesEntry.SetText(strconv.FormatFloat(params.Satellite.MajorAxisPaDegrees, 'f', -1, 64))
				pathToExternalImageEntry.SetText(params.PathToExternalImage)
				loadedFileName = filepath.Base(lastLoadedParamsPath)
			}
		}
	}

	// Helper to wrap entry in a fixed-width container
	entryWidth := float32(280)
	wrapEntry := func(e *widget.Entry) *fyne.Container {
		return container.New(layout.NewGridWrapLayout(fyne.NewSize(entryWidth, 36)), e)
	}

	// Create a left column form
	leftForm := widget.NewForm(
		&widget.FormItem{Text: "Window Size (pixels)", Widget: wrapEntry(windowSizeEntry)},
		&widget.FormItem{Text: "Title", Widget: wrapEntry(titleEntry)},
		&widget.FormItem{Text: "Fund. Plane Width (km)", Widget: wrapEntry(fundamentalPlaneWidthKmEntry)},
		&widget.FormItem{Text: "Fund. Plane Width (pts)", Widget: wrapEntry(fundamentalPlaneWidthNumPointsEntry)},
		&widget.FormItem{Text: "Parallax (arcsec)", Widget: wrapEntry(parallaxArcsecEntry)},
		&widget.FormItem{Text: "Distance (AU)", Widget: wrapEntry(distanceAuEntry)},
		&widget.FormItem{Text: "Path to QE Table File", Widget: wrapEntry(pathToQeTableFileEntry)},
		&widget.FormItem{Text: "Obs. Wavelength (nm)", Widget: wrapEntry(observationWavelengthNmEntry)},
		&widget.FormItem{Text: "dX (km/sec)", Widget: wrapEntry(dXKmPerSecEntry)},
		&widget.FormItem{Text: "dY (km/sec)", Widget: wrapEntry(dYKmPerSecEntry)},
		&widget.FormItem{Text: "Path Perp. Offset (km)", Widget: wrapEntry(pathPerpendicularOffsetKmEntry)},
		&widget.FormItem{Text: "Percent Mag Drop", Widget: wrapEntry(percentMagDropEntry)},
		&widget.FormItem{Text: "Star Diam. (mas)", Widget: wrapEntry(starDiamOnPlaneMasEntry)},
	)

	// Create a right column form
	rightForm := widget.NewForm(
		&widget.FormItem{Text: "Limb Darkening Coeff", Widget: wrapEntry(limbDarkeningCoeffEntry)},
		&widget.FormItem{Text: "Star Class", Widget: wrapEntry(starClassEntry)},
		&widget.FormItem{Text: "Main Body X (km)", Widget: wrapEntry(mainBodyXCenterEntry)},
		&widget.FormItem{Text: "Main Body Y (km)", Widget: wrapEntry(mainBodyYCenterEntry)},
		&widget.FormItem{Text: "Main Body Major (km)", Widget: wrapEntry(mainBodyMajorAxisEntry)},
		&widget.FormItem{Text: "Main Body Minor (km)", Widget: wrapEntry(mainBodyMinorAxisEntry)},
		&widget.FormItem{Text: "Main Body PA (deg)", Widget: wrapEntry(mainBodyPaDegreesEntry)},
		&widget.FormItem{Text: "Satellite X (km)", Widget: wrapEntry(satelliteXCenterEntry)},
		&widget.FormItem{Text: "Satellite Y (km)", Widget: wrapEntry(satelliteYCenterEntry)},
		&widget.FormItem{Text: "Satellite Major (km)", Widget: wrapEntry(satelliteMajorAxisEntry)},
		&widget.FormItem{Text: "Satellite Minor (km)", Widget: wrapEntry(satelliteMinorAxisEntry)},
		&widget.FormItem{Text: "Satellite PA (deg)", Widget: wrapEntry(satellitePaDegreesEntry)},
		&widget.FormItem{Text: "External Image Path", Widget: wrapEntry(pathToExternalImageEntry)},
	)

	// Create a two-column layout
	twoColumns := container.NewHBox(
		container.NewPadded(leftForm),
		container.NewPadded(rightForm),
	)

	scrollContent := container.NewVScroll(twoColumns)
	scrollContent.SetMinSize(fyne.NewSize(770, 650))

	// Create a custom dialog with OK/Cancel buttons
	var customDialog *dialog.CustomDialog
	okBtn := widget.NewButton("OK", func() {
		// TODO: Process the form data
		customDialog.Hide()
	})
	okBtn.Importance = widget.HighImportance
	cancelBtn := widget.NewButton("Cancel", func() {
		customDialog.Hide()
	})

	// File open button
	loadBtn := widget.NewButton("Load...", func() {
		fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if reader == nil {
				return // User cancelled
			}
			defer func() {
				if cerr := reader.Close(); cerr != nil {
					dialog.ShowError(fmt.Errorf("failed to close file: %w", cerr), w)
				}
			}()

			params, err := parseOccultationParameters(reader)
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load parameters: %w", err), w)
				return
			}

			// Populate entry fields with loaded values
			windowSizeEntry.SetText(strconv.Itoa(params.WindowSizePixels))
			titleEntry.SetText(params.Title)
			fundamentalPlaneWidthKmEntry.SetText(strconv.FormatFloat(params.FundamentalPlaneWidthKm, 'f', -1, 64))
			fundamentalPlaneWidthNumPointsEntry.SetText(strconv.Itoa(params.FundamentalPlaneWidthNumPoints))
			parallaxArcsecEntry.SetText(strconv.FormatFloat(params.ParallaxArcsec, 'f', -1, 64))
			distanceAuEntry.SetText(strconv.FormatFloat(params.DistanceAu, 'f', -1, 64))
			pathToQeTableFileEntry.SetText(params.PathToQeTableFile)
			observationWavelengthNmEntry.SetText(strconv.Itoa(params.ObservationWavelengthNm))
			dXKmPerSecEntry.SetText(strconv.FormatFloat(params.DXKmPerSec, 'f', -1, 64))
			dYKmPerSecEntry.SetText(strconv.FormatFloat(params.DYKmPerSec, 'f', -1, 64))
			pathPerpendicularOffsetKmEntry.SetText(strconv.FormatFloat(params.PathPerpendicularOffsetKm, 'f', -1, 64))
			percentMagDropEntry.SetText(strconv.Itoa(params.PercentMagDrop))
			starDiamOnPlaneMasEntry.SetText(strconv.FormatFloat(params.StarDiamOnPlaneMas, 'f', -1, 64))
			limbDarkeningCoeffEntry.SetText(strconv.FormatFloat(params.LimbDarkeningCoeff, 'f', -1, 64))
			starClassEntry.SetText(params.StarClass)
			mainBodyXCenterEntry.SetText(strconv.FormatFloat(params.MainBody.XCenterKm, 'f', -1, 64))
			mainBodyYCenterEntry.SetText(strconv.FormatFloat(params.MainBody.YCenterKm, 'f', -1, 64))
			mainBodyMajorAxisEntry.SetText(strconv.FormatFloat(params.MainBody.MajorAxisKm, 'f', -1, 64))
			mainBodyMinorAxisEntry.SetText(strconv.FormatFloat(params.MainBody.MinorAxisKm, 'f', -1, 64))
			mainBodyPaDegreesEntry.SetText(strconv.FormatFloat(params.MainBody.MajorAxisPaDegrees, 'f', -1, 64))
			satelliteXCenterEntry.SetText(strconv.FormatFloat(params.Satellite.XCenterKm, 'f', -1, 64))
			satelliteYCenterEntry.SetText(strconv.FormatFloat(params.Satellite.YCenterKm, 'f', -1, 64))
			satelliteMajorAxisEntry.SetText(strconv.FormatFloat(params.Satellite.MajorAxisKm, 'f', -1, 64))
			satelliteMinorAxisEntry.SetText(strconv.FormatFloat(params.Satellite.MinorAxisKm, 'f', -1, 64))
			satellitePaDegreesEntry.SetText(strconv.FormatFloat(params.Satellite.MajorAxisPaDegrees, 'f', -1, 64))
			pathToExternalImageEntry.SetText(params.PathToExternalImage)

			// Store the loaded file name for use as default in the save dialog
			loadedFileName = reader.URI().Name()
			// Store the full path for use by Run IOTAdiffraction
			lastLoadedParamsPath = reader.URI().Path()
		}, w)
		fileDialog.SetFilter(nil) // Allow all files or set a specific filter
		fileDialog.Resize(fyne.NewSize(1200, 800))
		fileDialog.Show()
	})

	// Helper functions to parse entry values
	parseFloat := func(s string) float64 {
		v, _ := strconv.ParseFloat(s, 64)
		return v
	}
	parseInt := func(s string) int {
		v, _ := strconv.Atoi(s)
		return v
	}

	// File save button
	saveBtn := widget.NewButton("Save...", func() {
		fileDialog := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if writer == nil {
				return // User cancelled
			}
			defer func() {
				if cerr := writer.Close(); cerr != nil {
					dialog.ShowError(fmt.Errorf("failed to close file: %w", cerr), w)
				}
			}()

			// Build parameters struct from entry fields
			params := OccultationParameters{
				WindowSizePixels:               parseInt(windowSizeEntry.Text),
				Title:                          titleEntry.Text,
				FundamentalPlaneWidthKm:        parseFloat(fundamentalPlaneWidthKmEntry.Text),
				FundamentalPlaneWidthNumPoints: parseInt(fundamentalPlaneWidthNumPointsEntry.Text),
				ParallaxArcsec:                 parseFloat(parallaxArcsecEntry.Text),
				DistanceAu:                     parseFloat(distanceAuEntry.Text),
				PathToQeTableFile:              pathToQeTableFileEntry.Text,
				ObservationWavelengthNm:        parseInt(observationWavelengthNmEntry.Text),
				DXKmPerSec:                     parseFloat(dXKmPerSecEntry.Text),
				DYKmPerSec:                     parseFloat(dYKmPerSecEntry.Text),
				PathPerpendicularOffsetKm:      parseFloat(pathPerpendicularOffsetKmEntry.Text),
				PercentMagDrop:                 parseInt(percentMagDropEntry.Text),
				StarDiamOnPlaneMas:             parseFloat(starDiamOnPlaneMasEntry.Text),
				LimbDarkeningCoeff:             parseFloat(limbDarkeningCoeffEntry.Text),
				StarClass:                      starClassEntry.Text,
				MainBody: EllipseParams{
					XCenterKm:          parseFloat(mainBodyXCenterEntry.Text),
					YCenterKm:          parseFloat(mainBodyYCenterEntry.Text),
					MajorAxisKm:        parseFloat(mainBodyMajorAxisEntry.Text),
					MinorAxisKm:        parseFloat(mainBodyMinorAxisEntry.Text),
					MajorAxisPaDegrees: parseFloat(mainBodyPaDegreesEntry.Text),
				},
				Satellite: EllipseParams{
					XCenterKm:          parseFloat(satelliteXCenterEntry.Text),
					YCenterKm:          parseFloat(satelliteYCenterEntry.Text),
					MajorAxisKm:        parseFloat(satelliteMajorAxisEntry.Text),
					MinorAxisKm:        parseFloat(satelliteMinorAxisEntry.Text),
					MajorAxisPaDegrees: parseFloat(satellitePaDegreesEntry.Text),
				},
				PathToExternalImage: pathToExternalImageEntry.Text,
			}

			// Marshal to JSON5
			data, err := json5.Marshal(params)
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to encode parameters: %w", err), w)
				return
			}

			// Indent the JSON5 output
			var indented []byte
			if err := json5.Indent(&indented, data, "", "  "); err != nil {
				dialog.ShowError(fmt.Errorf("failed to format parameters: %w", err), w)
				return
			}
			data = indented

			// Write to the file
			if _, err := writer.Write(data); err != nil {
				dialog.ShowError(fmt.Errorf("failed to write file: %w", err), w)
				return
			}

			// Close the parameters dialog after a successful save
			customDialog.Hide()
		}, w)
		fileDialog.SetFilter(nil) // Allow all files or set a specific filter
		if loadedFileName != "" {
			fileDialog.SetFileName(loadedFileName)
		}
		fileDialog.Resize(fyne.NewSize(1200, 800))
		fileDialog.Show()
	})

	buttons := container.NewHBox(loadBtn, saveBtn, layout.NewSpacer(), cancelBtn, okBtn)
	content := container.NewBorder(nil, buttons, nil, nil, scrollContent)

	customDialog = dialog.NewCustomWithoutButtons("Occultation Parameters", content, w)
	customDialog.Resize(fyne.NewSize(840, 750))
	customDialog.Show()
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
	w := a.NewWindow("GoPyOTE Version: " + Version)

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
	// Helper function to run IOTAdiffraction with a given parameter file
	runIOTAdiffraction := func(paramFilePath string) {
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

		// Set up the command with pipes using the selected file as a parameter
		cmd := exec.Command(exePath, paramFilePath)
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
	}

	btnIOTA := widget.NewButton("Run IOTAdiffraction", func() {
		// If a parameters file was previously loaded, use it directly
		if lastLoadedParamsPath != "" {
			runIOTAdiffraction(lastLoadedParamsPath)
			return
		}

		// Otherwise, open the file selection dialog to choose a parameter file
		fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if reader == nil {
				return // User cancelled
			}
			// Get the file path and close the reader (we don't need to read the content)
			paramFilePath := reader.URI().Path()
			if cerr := reader.Close(); cerr != nil {
				dialog.ShowError(fmt.Errorf("failed to close file: %w", cerr), w)
			}

			runIOTAdiffraction(paramFilePath)
		}, w)
		fileDialog.Resize(fyne.NewSize(1200, 800))
		fileDialog.Show()
	})
	btnOccultParams := widget.NewButton("Occultation Parameters", func() {
		showOccultationParametersDialog(w)
	})
	buttons := container.NewHBox(btn1, btn2, btn3, btn4, btnIOTA, btnOccultParams)

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
