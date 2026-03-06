package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"image/color"
)

// tabBgEntry tracks a tab background rectangle with its light/dark mode colors.
type tabBgEntry struct {
	rect       *canvas.Rectangle
	lightColor color.RGBA
	darkColor  color.RGBA
}

// appContext holds shared state that is referenced across multiple tabs.
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
	rebuildPlot      func()
	toggleLightCurve func(columnIndex int)

	// Callbacks set during tab construction
	resetFitButtons         func()
	resetNormalizeBtn       func()
	enablePostFitButtons    func()
	resetProcessOccelmntBtn func()
	resetIOTABtn            func()
	enableShowIOTAPlots     func()
	autoFillSearchRange     func()
}

// makeTabBg creates a colored background rectangle and registers it for dark-mode toggling.
func (ac *appContext) makeTabBg(light, dark color.RGBA) *canvas.Rectangle {
	rect := canvas.NewRectangle(light)
	ac.tabBgs = append(ac.tabBgs, tabBgEntry{rect, light, dark})
	return rect
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
