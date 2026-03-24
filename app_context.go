package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
)

// tabBgEntry tracks a tab background rectangle with its light/dark mode colors.
type tabBgEntry struct {
	rect       *canvas.Rectangle
	lightColor color.RGBA
	darkColor  color.RGBA
}

// appContext holds a shared state referenced across multiple tabs.
// Created once in main() and passed to tab builder functions.
type appContext struct {
	window fyne.Window
	app    fyne.App
	prefs  fyne.Preferences

	lightCurvePlot *LightCurvePlot
	vizierTab      *VizieRTab

	// Plot-area controls shared by multiple tabs
	startFrameEntry *FocusLossEntry
	endFrameEntry   *FocusLossEntry

	// Mutable plot state shared across tabs
	smoothedSeries    *PlotSeries
	theorySeries      *PlotSeries
	trendSeries       *PlotSeries // DEBUG: Savitzky-Golay trend from correlated noise analysis
	frameRangeStart   float64
	frameRangeEnd     float64
	minFrameNum       float64
	maxFrameNum       float64
	displayedCurves   map[int]bool
	curveColors       []color.RGBA
	currentXAxisLabel string

	// Tab background management
	tabBgs []tabBgEntry

	// Callbacks set by builders, called by other tabs or main
	rebuildPlot      func(...func())
	toggleLightCurve func(columnIndex int)

	// Callbacks set during tab construction
	resetFitTab              func() // full reset of the Fit tab state when a light curve changes
	invalidateFitCurves      func() // clear cached fit results (diffraction images changed) but keep the baseline state
	resetFitButtons          func()
	resetNormalizeBtn        func()
	enablePostFitButtons     func()
	resetProcessOccelmntBtn  func()
	enableShowIOTAPlots      func()
	autoFillSearchRange      func()
	stopProcessOccelmntBlink func()
	startAcqTimingBlink      func()
	stopAcqTimingBlink       func()
	confirmAcqTiming         func() // mark Image Acq Timing as confirmed (button orange, no blink)
	selectFitTab             func()
	selectVizierTab          func()
	markVizierWritten        func() // turn VizieR button orange after .dat export

	// NIE manual selection mode flag — true when the checkbox is checked.
	nieManualSelectMode bool

	// Show diagnostics plots flag — checked on the Settings tab, read by Fit tab.
	showDiagnostics bool

	// Use correlated noise flag — checked on the Settings tab, read by Fit tab.
	useCorrelatedNoise bool

	// AR model parameters fitted from pre-detrend autocorrelation (rho).
	// Set during baseline normalization; used by Monte Carlo and NIE when
	// useCorrelatedNoise is true.
	arPhi    []float64
	arSigma2 float64

	// suppressBusyDialog, when true, skips the "Redrawing plot" dialog for
	// the next rebuildPlot call. Automatically cleared after use.
	suppressBusyDialog bool

	// updateSodisComment is set by the SODIS dialog to allow external callers
	// (e.g., Image Acquisition Timing) to update the Comments field.
	updateSodisComment func(string)
}

// makeTabBg creates a colored background rectangle and registers it for dark-mode toggling.
func (ac *appContext) makeTabBg(light, dark color.RGBA) *canvas.Rectangle {
	rect := canvas.NewRectangle(light)
	ac.tabBgs = append(ac.tabBgs, tabBgEntry{rect, light, dark})
	return rect
}

// overlayTheoryCurve sets the theoretical light curve and edge lines on the main plot
// from a fitResult. If edgeStds is non-nil, ±3σ sigma lines are also drawn.
func (ac *appContext) overlayTheoryCurve(fr *fitResult, edgeStds []float64) {
	scale := fr.bestScale
	if scale == 0 {
		scale = 1.0
	}
	theoryPoints := make([]PlotPoint, len(fr.curve))
	for i, pt := range fr.curve {
		theoryPoints[i] = PlotPoint{
			X:     pt.time + fr.bestShift,
			Y:     pt.intensity*scale + (1.0 - scale),
			Index: -1,
		}
	}
	ac.theorySeries = &PlotSeries{
		Points:   theoryPoints,
		Color:    color.RGBA{R: 255, G: 170, B: 170, A: 255},
		Name:     "Theoretical (fit)",
		LineOnly: true,
	}
	edgeXVals := make([]float64, len(fr.edgeTimes))
	for i, et := range fr.edgeTimes {
		edgeXVals[i] = et + fr.bestShift
	}
	ac.lightCurvePlot.SetVerticalLines(edgeXVals, true)

	var sigmaXVals []float64
	if len(edgeStds) > 0 {
		for i, et := range fr.edgeTimes {
			if i < len(edgeStds) {
				edgeX := et + fr.bestShift
				sigma3 := 3.0 * edgeStds[i]
				sigmaXVals = append(sigmaXVals, edgeX-sigma3, edgeX+sigma3)
			}
		}
	}
	ac.lightCurvePlot.SetSigmaLines(sigmaXVals, len(sigmaXVals) > 0)
	ac.lightCurvePlot.ShowBaselineLine = false
	savedMinY, savedMaxY := ac.lightCurvePlot.GetYBounds()
	ac.rebuildPlot(func() {
		ac.lightCurvePlot.SetYBounds(savedMinY, savedMaxY)
	})
}

// applyTabBgTheme switches all registered tab backgrounds between light and dark mode.
func (ac *appContext) applyTabBgTheme(isDark bool) {
	for _, entry := range ac.tabBgs {
		if isDark {
			entry.rect.FillColor = entry.darkColor
		} else {
			entry.rect.FillColor = entry.lightColor
		}
		entry.rect.Refresh()
	}
}
