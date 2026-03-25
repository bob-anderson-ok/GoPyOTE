package main

import (
	"bufio"
	"bytes"
	"embed"
	"encoding/csv"
	"fmt"
	"image/color"
	"image/png"
	"io"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"GoPyOTE/lightcurve"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/KevinWang15/go-json5"
)

//go:embed help_markdown/smoothingAndNormalization.md help_images/SmoothingFig1.png help_images/SmoothingFig2.png help_images/SmoothingFig3.png help_images/SmoothingFig4.png
var smoothingMarkdown embed.FS

//go:embed help_markdown/timestampAnalysis.md help_images/droppedFrameDemoPlot.png help_images/consecutiveOCRerrorDemo.png
var timestampAnalysisMarkdown embed.FS

//go:embed help_markdown/blockIntegration.md
var blockIntegrationMarkdown embed.FS

//go:embed help_markdown/about.md
var aboutMarkdown embed.FS

//go:embed help_markdown/vizierMarkdown.md
var vizierExportMarkdown embed.FS

//go:embed help_markdown/fitMarkdown.md
var fitExplanationMarkdown embed.FS

//go:embed help_markdown/occelmntOWC.md
var occelmntButtonExplanation embed.FS

//go:embed help_markdown/editOccParams.md
var editOccParamsExplanation embed.FS

//go:embed help_markdown/fresnelScaleResolution.md
var fresnelScaleResolutionMarkdown embed.FS

//go:embed help_markdown/edgeTimeSigmaExplanation.md
var monteCarloExplanation embed.FS

//go:embed help_markdown help_images
var correlatedNoiseExplanation embed.FS

// Version information
const Version = "1.2.55"

// Track the last loaded parameters file path for use by IOTAdiffraction
var lastLoadedParamsPath string

// Track the last loaded site file path for use by the Fill SODIS Report dialog
var lastLoadedSitePath string

// Track the last loaded occelmnt XML text for use by the Fill SODIS Report dialog
var lastLoadedOccelmntXml string

// Track the CSV-measured median exposure time (seconds) for use by the Fill SODIS Report dialog
var lastCsvExposureSecs float64

// Track the observer GPS location from the last successful ObserverT0CorrectionFromOWC call.
// Persisted in preferences so the SODIS fill works across sessions.
var lastObserverLatDeg float64
var lastObserverLonDeg float64
var lastObserverAltMeters float64
var lastObserverLocationSet bool

// Track the parameters file used for the last IOTAdiffraction run (for startup display)
var lastDiffractionParamsPath string

// Title from the parameters file used for the last IOTAdiffraction run (for plot titles)
var lastDiffractionTitle string

// normalizeAsteroidTitle replaces "(0)" with "(-)" in titles where the asteroid number is 0.
func normalizeAsteroidTitle(title string) string {
	if strings.HasPrefix(title, "(0)") {
		return "(-)" + title[3:]
	}
	return title
}

// resultsFolder is the path to the -RESULTS folder created alongside the opened CSV file.
// Various outputs (fit plots, histograms, etc.) are written here.
var resultsFolder string

// sodisReportSavedThisSession is set to true when a SODIS report is saved
// during the current session, so that the VizieR "Copy from SODIS-REPORT.txt"
// button only accepts a report generated in this session.
var sodisReportSavedThisSession bool
var vizierDatWrittenThisSession bool

// sodisNegativeReportSaved is set to true when a NEGATIVE SODIS report is saved.
// A negative report does not require a VizieR .dat file, so the close warning is skipped.
var sodisNegativeReportSaved bool

// Session-only variables for Image Acquisition Timing dialog values.
// These are populated from camera-timing.txt when an observation folder is loaded,
// or set when the user clicks OK in the dialog.
var sessionStarRow string
var sessionAcqDelay string
var sessionRowDelta string
var sessionCameraName string

// onVizierDatWritten is called after a VizieR .dat file is successfully written.
// Set by buildFitTab to turn the VizieR button orange.
var onVizierDatWritten func()

// occultationProcessedForCurrentCSV is set to true when prior diffraction results
// are found in the -RESULTS folder or when IOTAdiffraction runs for the current CSV.
// Reset to false each time a new CSV is loaded.
var occultationProcessedForCurrentCSV bool

// afterOccParamsSaved, when non-nil, is called with the saved file path immediately after
// showOccultationParametersDialog successfully writes a new .occparams file.
// Assigned in main() once all UI elements needed by IOTAdiffraction are initialized.
var afterOccParamsSaved func(string)

// iotaDiffractionRunning is true while IOTAdiffraction.exe is executing.
// Used to prevent fit searches from running against stale diffraction images.
var iotaDiffractionRunning atomic.Bool

// appDir is the directory containing the executable. Used to resolve relative file paths
// (diffraction images, IOTAdiffraction.exe, etc.) regardless of the OS working directory.
var appDir string

// grayPlotBackground controls whether plots use a gray background instead of white.
var grayPlotBackground bool

// plotBackgroundColor returns the color to use for plot backgrounds based on the preference.
var plotBackgroundGray = color.RGBA{R: 170, G: 170, B: 170, A: 255}

func main() {
	// Declared early so all closures throughout main() can reference it;
	// initialized below after the tab structure is ready.
	var vizierTab *VizieRTab

	// Determine the directory containing the executable so that relative file
	// references (diffraction images, IOTAdiffraction.exe, etc.) resolve correctly
	// regardless of how the program is launched (e.g., from an IDE).
	if exePath, err := os.Executable(); err == nil {
		appDir = filepath.Dir(exePath)
	} else {
		appDir, _ = os.Getwd()
	}

	a := app.NewWithID("com.gopyote.app")
	w := a.NewWindow("GoPyOTE Version: " + Version)
	w.SetMaster() // Closing this window will quit the app and close all other windows

	// Load saved window geometry
	prefs := a.Preferences()

	// Create the shared application context
	ac := &appContext{
		window:          w,
		app:             a,
		prefs:           prefs,
		displayedCurves: make(map[int]bool),
	}

	// Apply persisted dark mode preference
	if prefs.BoolWithFallback("darkMode", false) {
		a.Settings().SetTheme(&ForcedVariantTheme{Base: theme.DefaultTheme(), Variant: theme.VariantDark})
	} else {
		a.Settings().SetTheme(&ForcedVariantTheme{Base: theme.DefaultTheme(), Variant: theme.VariantLight})
	}

	// Apply persisted gray plot background preference
	grayPlotBackground = prefs.BoolWithFallback("grayPlotBackground", false)
	lightcurve.GrayPlotBackground = grayPlotBackground

	// Initialize preferences on the first run to avoid EOF errors
	if prefs.Int("initialized") == 0 {
		prefs.SetInt("initialized", 1)
		prefs.SetInt("windowX", -1)
		prefs.SetInt("windowY", -1)
		prefs.SetInt("windowW", 1000)
		prefs.SetInt("windowH", 600)
		prefs.SetFloat("splitOffset", 0.6)
	}

	// Purge star row from preferences — it is session-only now.
	prefs.RemoveValue("imageAcqStarRow")

	// Load the last used parameters path from preferences for startup display
	lastLoadedParamsPath = prefs.StringWithFallback("lastLoadedParamsPath", "")
	// Restore last loaded occelmnt XML text
	lastLoadedOccelmntXml = prefs.StringWithFallback("lastLoadedOccelmntXml", "")
	// Restore observer GPS location for SODIS fill
	lastObserverLocationSet = prefs.BoolWithFallback("lastObserverLocationSet", false)
	if lastObserverLocationSet {
		lastObserverLatDeg = prefs.FloatWithFallback("lastObserverLatDeg", 0.0)
		lastObserverLonDeg = prefs.FloatWithFallback("lastObserverLonDeg", 0.0)
		lastObserverAltMeters = prefs.FloatWithFallback("lastObserverAltMeters", 0.0)
	}
	lastDiffractionParamsPath = prefs.StringWithFallback("lastDiffractionParamsPath", "")
	if lastDiffractionParamsPath != "" {
		logOccparamsRead("startup restore", lastDiffractionParamsPath)
	}
	lastDiffractionTitle = normalizeAsteroidTitle(prefs.StringWithFallback("lastDiffractionTitle", ""))
	// Backfill the title from the parameters file if a path exists but title was never saved
	if lastDiffractionParamsPath != "" && lastDiffractionTitle == "" {
		if f, err := os.Open(lastDiffractionParamsPath); err == nil {
			if p, err := parseOccultationParameters(f); err == nil && p.Title != "" {
				lastDiffractionTitle = normalizeAsteroidTitle(p.Title)
				prefs.SetString("lastDiffractionTitle", lastDiffractionTitle)
			}
			if err := f.Close(); err != nil {
				fmt.Printf("Warning: failed to close parameters file: %v\n", err)
			}
		}
	}

	savedX := int32(prefs.IntWithFallback("windowX", -1))
	savedY := int32(prefs.IntWithFallback("windowY", -1))
	savedW := int32(prefs.IntWithFallback("windowW", 1000))
	savedH := int32(prefs.IntWithFallback("windowH", 600))
	firstRun := savedX == -1 && savedY == -1

	// Create a menu
	helpMenu := fyne.NewMenu("Help Topics",
		fyne.NewMenuItem("Video library link", func() {
			u, _ := url.Parse("https://github.com/bob-anderson-ok/GoPyOTE-Videos/releases")
			if err := a.OpenURL(u); err != nil {
				dialog.ShowError(fmt.Errorf("failed to open Video library link: %w", err), w)
			}
		}),
		fyne.NewMenuItem("Block Integration", func() {
			content, err := blockIntegrationMarkdown.ReadFile("help_markdown/blockIntegration.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load blockIntegration.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("Block Integration", string(content), &blockIntegrationMarkdown, w)
		}),
		fyne.NewMenuItem("Smoothing and Normalization", func() {
			content, err := smoothingMarkdown.ReadFile("help_markdown/smoothingAndNormalization.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load smoothingAndNormalization.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("Smoothing and Normalization", string(content), &smoothingMarkdown, w)
		}),
		fyne.NewMenuItem("Dropped frames and OCR issues", func() {
			content, err := timestampAnalysisMarkdown.ReadFile("help_markdown/timestampAnalysis.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load timestampAnalysis_old.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("Dropped frames and OCR issues", string(content), &timestampAnalysisMarkdown, w)
		}),
		fyne.NewMenuItem("VizieR export", func() {
			content, err := vizierExportMarkdown.ReadFile("help_markdown/vizierMarkdown.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load vizierMarkdown.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("VizieR export", string(content), &vizierExportMarkdown, w)
		}),
		fyne.NewMenuItem("Fit explanation", func() {
			content, err := fitExplanationMarkdown.ReadFile("help_markdown/fitMarkdown.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load fitMarkdown.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("Fit explanation", string(content), &fitExplanationMarkdown, w)
		}),
		fyne.NewMenuItem("Process OWC occelmnt.xml", func() {
			content, err := occelmntButtonExplanation.ReadFile("help_markdown/occelmntOWC.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load occelmntOWC.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("Process OWC occelmnt.xml", string(content), &occelmntButtonExplanation, w)
		}),
		fyne.NewMenuItem("Edit Occ Params", func() {
			content, err := editOccParamsExplanation.ReadFile("help_markdown/editOccParams.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load editOccParams.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("Edit Occ Params", string(content), &editOccParamsExplanation, w)
		}),
		fyne.NewMenuItem("Fresnel scale resolution", func() {
			content, err := fresnelScaleResolutionMarkdown.ReadFile("help_markdown/fresnelScaleResolution.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load fresnelScaleResolution.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("Fresnel scale resolution", string(content), &fresnelScaleResolutionMarkdown, w)
		}),
		fyne.NewMenuItem("Monte Carlo sigma estimation", func() {
			content, err := monteCarloExplanation.ReadFile("help_markdown/edgeTimeSigmaExplanation.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load edgeTimeSigmaExplanation.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("Monte Carlo sigma estimation", string(content), &monteCarloExplanation, w)
		}),
		fyne.NewMenuItem("Correlated Noise", func() {
			content, err := correlatedNoiseExplanation.ReadFile("help_markdown/correlatedNoise.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load correlatedNoise.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("Correlated Noise", string(content), &correlatedNoiseExplanation, w)
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("About GoPyOTE", func() {
			content, err := aboutMarkdown.ReadFile("help_markdown/about.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load about.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("About GoPyOTE", string(content), &aboutMarkdown, w)
		}),
	)
	mainMenu := fyne.NewMainMenu(helpMenu)
	w.SetMainMenu(mainMenu)

	// Tab 2: Settings
	// Track which light curve prefixes to include when loading CSV
	lightCurvePrefixes := map[string]bool{
		"signal":     true,
		"appsum":     false,
		"avgbkg":     false,
		"stdbkg":     false,
		"nmaskpx":    false,
		"maxpx":      false,
		"xcentroid":  false,
		"ycentroid":  false,
		"hit-defect": false,
	}
	acceptAnyName := false

	// Function variable for refreshing the light curve list when filter changes (set later)
	var refreshLightCurveFilter func()

	// Create checkboxes for light curve prefixes
	signalCheck := widget.NewCheck("signal", func(checked bool) {
		lightCurvePrefixes["signal"] = checked
		if refreshLightCurveFilter != nil {
			refreshLightCurveFilter()
		}
	})
	signalCheck.SetChecked(true)
	appsumCheck := widget.NewCheck("appsum", func(checked bool) {
		lightCurvePrefixes["appsum"] = checked
		if refreshLightCurveFilter != nil {
			refreshLightCurveFilter()
		}
	})
	avgbkgCheck := widget.NewCheck("avgbkg", func(checked bool) {
		lightCurvePrefixes["avgbkg"] = checked
		if refreshLightCurveFilter != nil {
			refreshLightCurveFilter()
		}
	})
	stdbkgCheck := widget.NewCheck("stdbkg", func(checked bool) {
		lightCurvePrefixes["stdbkg"] = checked
		if refreshLightCurveFilter != nil {
			refreshLightCurveFilter()
		}
	})
	nmaskpxCheck := widget.NewCheck("nmaskpx", func(checked bool) {
		lightCurvePrefixes["nmaskpx"] = checked
		if refreshLightCurveFilter != nil {
			refreshLightCurveFilter()
		}
	})
	maxpxCheck := widget.NewCheck("maxpx", func(checked bool) {
		lightCurvePrefixes["maxpx"] = checked
		if refreshLightCurveFilter != nil {
			refreshLightCurveFilter()
		}
	})
	xcentroidCheck := widget.NewCheck("xcentroid", func(checked bool) {
		lightCurvePrefixes["xcentroid"] = checked
		if refreshLightCurveFilter != nil {
			refreshLightCurveFilter()
		}
	})
	ycentroidCheck := widget.NewCheck("ycentroid", func(checked bool) {
		lightCurvePrefixes["ycentroid"] = checked
		if refreshLightCurveFilter != nil {
			refreshLightCurveFilter()
		}
	})
	hitDefectCheck := widget.NewCheck("hit-defect", func(checked bool) {
		lightCurvePrefixes["hit-defect"] = checked
		if refreshLightCurveFilter != nil {
			refreshLightCurveFilter()
		}
	})
	anyNameCheck := widget.NewCheck("any name (use for Tangra)", func(checked bool) {
		acceptAnyName = checked
		if refreshLightCurveFilter != nil {
			refreshLightCurveFilter()
		}
	})

	prefixCheckboxes := container.NewVBox(
		widget.NewLabel("Light curve prefixes to include:"),
		anyNameCheck,
		signalCheck,
		appsumCheck,
		avgbkgCheck,
		stdbkgCheck,
		nmaskpxCheck,
		maxpxCheck,
		xcentroidCheck,
		ycentroidCheck,
		hitDefectCheck,
	)

	// makeTabBg and applyTabBgTheme are methods on ac (appContext)

	darkModeCheck := widget.NewCheck("Dark mode", func(checked bool) {
		if checked {
			a.Settings().SetTheme(&ForcedVariantTheme{Base: theme.DefaultTheme(), Variant: theme.VariantDark})
		} else {
			a.Settings().SetTheme(&ForcedVariantTheme{Base: theme.DefaultTheme(), Variant: theme.VariantLight})
		}
		ac.applyTabBgTheme(checked)
		prefs.SetBool("darkMode", checked)
	})
	darkModeCheck.Checked = prefs.BoolWithFallback("darkMode", false)

	grayBgCheck := widget.NewCheck("Gray plot backgrounds", func(checked bool) {
		grayPlotBackground = checked
		lightcurve.GrayPlotBackground = checked
		prefs.SetBool("grayPlotBackground", checked)
	})
	grayBgCheck.Checked = prefs.BoolWithFallback("grayPlotBackground", false)

	// Checkbox for timestamp tick format (callback set later after ac.lightCurvePlot is created)
	timestampTicksCheck := widget.NewCheck("Use timestamp format to display time value", nil)
	timestampTicksCheck.Checked = true

	showIOTAPlotsCheck := widget.NewCheck("Show plots from IOTAdiffraction", func(checked bool) {
		prefs.SetBool("showIOTAPlots", checked)
	})
	showIOTAPlotsCheck.Checked = prefs.BoolWithFallback("showIOTAPlots", true)

	showDiagnosticsCheck := widget.NewCheck("Show diagnostics plots (Fit tab)", func(checked bool) {
		ac.showDiagnostics = checked
	})
	showDiagnosticsCheck.Checked = false

	useCorrelatedNoiseCheck := widget.NewCheck("Use correlated noise", func(checked bool) {
		ac.useCorrelatedNoise = checked
	})
	useCorrelatedNoiseCheck.Checked = true
	ac.useCorrelatedNoise = true

	obsHomeDirEntry := widget.NewEntry()
	obsHomeDirEntry.SetPlaceHolder("Path to your observations folder...")
	obsHomeDirEntry.SetText(prefs.StringWithFallback("obsHomeDir", ""))
	obsHomeDirEntry.OnChanged = func(s string) {
		prefs.SetString("obsHomeDir", s)
	}
	obsHomeDirBrowseBtn := widget.NewButton("Browse", func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if uri == nil {
				return
			}
			obsHomeDirEntry.SetText(uri.Path())
		}, w)
	})
	obsHomeDirBrowseBtn.Importance = widget.HighImportance
	obsHomeDirBox := container.NewVBox(
		widget.NewLabel("Observation home directory:"),
		container.NewBorder(nil, nil, nil, obsHomeDirBrowseBtn, obsHomeDirEntry),
	)

	tab2Bg := ac.makeTabBg(color.RGBA{R: 200, G: 200, B: 230, A: 255}, color.RGBA{R: 50, G: 50, B: 80, A: 255})
	tab2Content := container.NewStack(tab2Bg, container.NewPadded(container.NewVBox(prefixCheckboxes, widget.NewSeparator(), darkModeCheck, grayBgCheck, timestampTicksCheck, showIOTAPlotsCheck, showDiagnosticsCheck, useCorrelatedNoiseCheck, widget.NewSeparator(), obsHomeDirBox)))
	tab2 := container.NewTabItem("Settings", tab2Content)

	// Create the plot area with an interactive light curve (before Tab 3 so it can be referenced)
	plotStatusLabel := widget.NewLabel("Click on a point to see details")
	plotStatusLabel.Wrapping = fyne.TextWrapWord

	// Frame number range entry boxes (created here so they can be in the plot area bottom)
	ac.startFrameEntry = NewFocusLossEntry()
	ac.startFrameEntry.SetPlaceHolder("Start Frame")
	ac.endFrameEntry = NewFocusLossEntry()
	ac.endFrameEntry.SetPlaceHolder("End Frame")
	startFrameContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(120, 36)), ac.startFrameEntry)
	endFrameContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(120, 36)), ac.endFrameEntry)

	// Trim entry boxes
	trimStartEntry := NewFocusLossEntry()
	trimStartEntry.SetPlaceHolder("Trim start")
	trimEndEntry := NewFocusLossEntry()
	trimEndEntry.SetPlaceHolder("Trim end")
	trimStartContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(120, 36)), trimStartEntry)
	trimEndContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(120, 36)), trimEndEntry)

	// Use a fixed-width label container so labels align vertically
	labelWidth := fyne.NewSize(80, 36)

	setTrimBtn := widget.NewButton("Set\ntrim", nil)
	setTrimBtn.Importance = widget.HighImportance
	trimPerformed = false

	trimStack := container.NewVBox(
		container.NewHBox(container.New(layout.NewGridWrapLayout(labelWidth), widget.NewLabel("Trim start:")), trimStartContainer),
		container.NewHBox(container.New(layout.NewGridWrapLayout(labelWidth), widget.NewLabel("Trim end:")), trimEndContainer),
	)
	applyTrimBtn := widget.NewButton("Show current trim", nil)
	applyTrimBtn.Importance = widget.HighImportance
	showAllBtn := widget.NewButton("Show all", nil)
	showAllBtn.Importance = widget.HighImportance
	trimBtnWidth := fyne.NewSize(150, 36)

	trimStackWithBtn := container.NewHBox(
		trimStack,
		container.New(layout.NewGridWrapLayout(fyne.NewSize(76, 76)), setTrimBtn),
		container.NewVBox(
			container.New(layout.NewGridWrapLayout(trimBtnWidth), applyTrimBtn),
			container.New(layout.NewGridWrapLayout(trimBtnWidth), showAllBtn),
		),
	)

	frameRangeRow := container.NewHBox(
		container.NewVBox(
			container.NewHBox(container.New(layout.NewGridWrapLayout(labelWidth), widget.NewLabel("Start frame:")), startFrameContainer),
			container.NewHBox(container.New(layout.NewGridWrapLayout(labelWidth), widget.NewLabel("End frame:")), endFrameContainer),
		),
		trimStackWithBtn,
	)

	// Track the current x-axis label for click callback
	ac.currentXAxisLabel = "Time"

	var onFitTab bool // Track whether the Fit tab is active

	// Create the plot with an empty series (will be populated when CSV is loaded)

	ac.lightCurvePlot = NewLightCurvePlot(nil, func(point PlotPoint) {
		if point.Series < 0 || point.Series >= len(ac.lightCurvePlot.series) {
			return
		}
		seriesName := ac.lightCurvePlot.series[point.Series].Name

		// Get frame number from loaded data
		frameNum := point.Index // fallback to index
		if loadedLightCurveData != nil && point.Index >= 0 && point.Index < len(loadedLightCurveData.FrameNumbers) {
			frameNum = int(loadedLightCurveData.FrameNumbers[point.Index])
		}

		// Format X value based on timestamp ticks setting
		var xValueStr string
		if ac.lightCurvePlot.GetUseTimestampTicks() {
			xValueStr = formatSecondsAsTimestamp(point.X)
		} else {
			xValueStr = fmt.Sprintf("%.4f", point.X)
		}

		plotStatusLabel.SetText(fmt.Sprintf("%s - Frame %d\n%s: %s\nValue: %.4f",
			seriesName, frameNum, ac.currentXAxisLabel, xValueStr, point.Y))
		logAction(fmt.Sprintf("Clicked point: %s Frame %d, %s=%s, Value=%.4f",
			seriesName, frameNum, ac.currentXAxisLabel, xValueStr, point.Y))

	})

	// Create X and Y axis range spinners (start empty, filled when the first curve selected)
	yMinEntry := NewFocusLossEntry()
	yMaxEntry := NewFocusLossEntry()
	yMinEntry.SetPlaceHolder("Y Min")
	yMaxEntry.SetPlaceHolder("Y Max")

	// Track if the user has manually set any bounds (don't reset on curve toggle)
	userSetBounds := false

	// Update entries when plot bounds change
	updateRangeEntries := func() {
		minY, maxY := ac.lightCurvePlot.GetYBounds()
		yMinEntry.SetText(fmt.Sprintf("%.4f", minY))
		yMaxEntry.SetText(fmt.Sprintf("%.4f", maxY))
	}
	// Don't call updateRangeEntries() here - wait until the first curve is selected

	// Handle Y Min entry changes
	yMinEntry.OnSubmitted = func(text string) {
		val, err := strconv.ParseFloat(text, 64)
		if err == nil {
			_, maxY := ac.lightCurvePlot.GetYBounds()
			ac.lightCurvePlot.SetYBounds(val, maxY)
			userSetBounds = true
			logAction(fmt.Sprintf("Set Y Min to %.4f", val))
		}
		updateRangeEntries()
	}

	// Handle Y Max entry changes
	yMaxEntry.OnSubmitted = func(text string) {
		val, err := strconv.ParseFloat(text, 64)
		if err == nil {
			minY, _ := ac.lightCurvePlot.GetYBounds()
			ac.lightCurvePlot.SetYBounds(minY, val)
			userSetBounds = true
			logAction(fmt.Sprintf("Set Y Max to %.4f", val))
		}
		updateRangeEntries()
	}

	// Wrap entries in containers (150px width)
	yMinContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(150, 36)), yMinEntry)
	yMaxContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(150, 36)), yMaxEntry)

	// Set the timestamp ticks callback now that ac.lightCurvePlot and updateRangeEntries exist
	timestampTicksCheck.OnChanged = func(checked bool) {
		ac.lightCurvePlot.SetUseTimestampTicks(checked)
		// Update the entry box format to match the new mode
		if len(ac.lightCurvePlot.series) > 0 {
			updateRangeEntries()
		}
		if checked {
			logAction("Enabled timestamp tick format")
		} else {
			logAction("Disabled timestamp tick format")
		}
	}
	ac.lightCurvePlot.SetUseTimestampTicks(true)

	// Create a toolbar with Y range controls
	rangeControls := container.NewHBox(
		widget.NewLabel("Y Min:"),
		yMinContainer,
		widget.NewLabel("Y Max:"),
		yMaxContainer,
		widget.NewButton("Clear marked points", func() {
			// Clear selected point 1
			ac.lightCurvePlot.selectedSeries = -1
			ac.lightCurvePlot.selectedIndex = -1
			ac.lightCurvePlot.selectedPointDataIndex = -1
			ac.lightCurvePlot.selectedSeriesName = ""
			ac.lightCurvePlot.SelectedPoint1Valid = false
			ac.lightCurvePlot.SelectedPoint1Frame = 0
			ac.lightCurvePlot.SelectedPoint1Value = 0

			// Clear selected point 2
			ac.lightCurvePlot.selectedSeries2 = -1
			ac.lightCurvePlot.selectedIndex2 = -1
			ac.lightCurvePlot.selectedPointDataIndex2 = -1
			ac.lightCurvePlot.selectedSeriesName2 = ""
			ac.lightCurvePlot.SelectedPoint2Valid = false
			ac.lightCurvePlot.SelectedPoint2Frame = 0
			ac.lightCurvePlot.SelectedPoint2Value = 0

			// Clear all selected pairs
			ac.lightCurvePlot.SelectedPairs = nil

			ac.lightCurvePlot.Refresh()
			plotStatusLabel.SetText("Click on a point to see details")
			logAction("Cleared all marked points")
		}),
	)

	// Bottom section with frame range controls on the left and status label on the right
	plotBottomRow := container.NewBorder(
		nil,             // top
		nil,             // bottom
		frameRangeRow,   // left
		nil,             // right
		plotStatusLabel, // center (takes remaining space)
	)

	plotCenter := container.NewStack(ac.lightCurvePlot)

	plotArea := container.NewBorder(
		rangeControls, // top
		plotBottomRow, // bottom
		nil,           // left
		nil,           // right
		plotCenter,    // center
	)

	// Tab 3: Data - Light curve list with click to toggle on/off plot
	tab3Bg := ac.makeTabBg(color.RGBA{R: 230, G: 220, B: 200, A: 255}, color.RGBA{R: 80, G: 70, B: 50, A: 255})

	// Create a list to display light curve column names
	var lightCurveListData []string         // Will be populated when CSV is loaded (filtered by prefixes)
	var listIndexToColumnIndex []int        // Maps list index to actual column index in data
	ac.displayedCurves = make(map[int]bool) // Track which curves are currently displayed (uses actual column indices)
	var lightCurveList *widget.List

	// Color palette for multiple light curves
	ac.curveColors = []color.RGBA{
		{R: 70, G: 130, B: 180, A: 255},  // Steel blue
		{R: 220, G: 120, B: 50, A: 255},  // Orange
		{R: 50, G: 180, B: 80, A: 255},   // Green
		{R: 180, G: 50, B: 180, A: 255},  // Purple
		{R: 200, G: 50, B: 50, A: 255},   // Red
		{R: 50, G: 180, B: 180, A: 255},  // Cyan
		{R: 180, G: 180, B: 50, A: 255},  // Yellow
		{R: 100, G: 100, B: 100, A: 255}, // Gray
	}

	// Savitzky-Golay smoothed series (nil if no smoothing has been applied)

	// Theoretical lightcurve series overlaid after a Monte Carlo run (nil if not set)

	// Track the current frame range for filtering plot data

	// Save min/max frame numbers from loaded CSV for validation

	// rebuildGeneration tracks the latest rebuildPlot invocation so that a
	// stale deferred apply (from the async "many points" path) is discarded
	// if a newer rebuildPlot call has already started.
	var rebuildGeneration int
	var busyDialog dialog.Dialog

	// Function to rebuild the plot with all currently displayed curves.
	// Optional afterApply callbacks run after SetSeries (useful for SetYBounds etc.).
	ac.rebuildPlot = func(afterApply ...func()) {
		if loadedLightCurveData == nil {
			return
		}
		rebuildGeneration++
		myGen := rebuildGeneration

		// Check if timestamps are empty (all zeros) - use frame numbers instead
		useFrameNumbers := true
		for _, t := range loadedLightCurveData.TimeValues {
			if t != 0 {
				useFrameNumbers = false
				break
			}
		}

		var allSeries []PlotSeries
		colorIdx := 0
		var displayedNames []string

		for colIdx := range loadedLightCurveData.Columns {
			if !ac.displayedCurves[colIdx] {
				continue
			}

			col := loadedLightCurveData.Columns[colIdx]
			var points []PlotPoint
			for i, val := range col.Values {
				// Filter by frame range if set
				frameNum := loadedLightCurveData.FrameNumbers[i]
				if ac.frameRangeStart > 0 && frameNum < ac.frameRangeStart {
					continue
				}
				if ac.frameRangeEnd > 0 && frameNum > ac.frameRangeEnd {
					continue
				}

				xVal := loadedLightCurveData.TimeValues[i]
				if useFrameNumbers {
					xVal = frameNum
				}
				points = append(points, PlotPoint{
					X:            xVal,
					Y:            val,
					Index:        i,
					Series:       len(allSeries),
					Interpolated: isInterpolatedIndex(i),
				})
			}

			if len(points) == 0 {
				continue // Skip series with no points in range
			}

			allSeries = append(allSeries, PlotSeries{
				Points: points,
				Color:  ac.curveColors[colorIdx%len(ac.curveColors)],
				Name:   col.Name,
			})
			displayedNames = append(displayedNames, col.Name)
			colorIdx++
		}

		// Set appropriate X axis label
		if useFrameNumbers {
			ac.currentXAxisLabel = "Frame Number"
			ac.lightCurvePlot.xAxisLabel = "Frame Number"
		} else {
			ac.currentXAxisLabel = "Time"
			ac.lightCurvePlot.xAxisLabel = "Time"
		}

		// Add smoothed series if available
		if ac.smoothedSeries != nil {
			// Rebuild smoothed series points with correct X values (frame numbers or timestamps)
			var smoothPoints []PlotPoint
			for _, pt := range ac.smoothedSeries.Points {
				// Filter by frame range
				frameNum := loadedLightCurveData.FrameNumbers[pt.Index]
				if ac.frameRangeStart > 0 && frameNum < ac.frameRangeStart {
					continue
				}
				if ac.frameRangeEnd > 0 && frameNum > ac.frameRangeEnd {
					continue
				}
				xVal := loadedLightCurveData.TimeValues[pt.Index]
				if useFrameNumbers {
					xVal = frameNum
				}
				smoothPoints = append(smoothPoints, PlotPoint{
					X:            xVal,
					Y:            pt.Y,
					Index:        pt.Index,
					Series:       len(allSeries),
					Interpolated: isInterpolatedIndex(pt.Index),
				})
			}
			if len(smoothPoints) > 0 {
				allSeries = append(allSeries, PlotSeries{
					Points: smoothPoints,
					Color:  ac.smoothedSeries.Color,
					Name:   ac.smoothedSeries.Name,
				})
				displayedNames = append(displayedNames, ac.smoothedSeries.Name)
			}
		}

		// Add theoretical lightcurve series if available (from the last Monte Carlo run)
		// Filter to only include points within the X range of the displayed light curve data.
		if ac.theorySeries != nil && len(allSeries) > 0 {
			// Determine the min and max X of all currently displayed light curve points
			xMin := math.Inf(1)
			xMax := math.Inf(-1)
			for _, s := range allSeries {
				for _, pt := range s.Points {
					if pt.X < xMin {
						xMin = pt.X
					}
					if pt.X > xMax {
						xMax = pt.X
					}
				}
			}
			var filteredPts []PlotPoint
			for _, pt := range ac.theorySeries.Points {
				if pt.X >= xMin && pt.X <= xMax {
					filteredPts = append(filteredPts, pt)
				}
			}
			if len(filteredPts) > 0 {
				allSeries = append(allSeries, PlotSeries{
					Points:   filteredPts,
					Color:    ac.theorySeries.Color,
					Name:     ac.theorySeries.Name,
					LineOnly: ac.theorySeries.LineOnly,
				})
			}
		}

		// Add sampled points on the theoretical curve (red dots), filtered to the visible X range
		if ac.theorySampledSeries != nil && len(allSeries) > 0 {
			xMin := math.Inf(1)
			xMax := math.Inf(-1)
			for _, s := range allSeries {
				if s.Name == "Theoretical (fit)" {
					continue // skip the theory line — use observation data bounds only
				}
				for _, pt := range s.Points {
					if pt.X < xMin {
						xMin = pt.X
					}
					if pt.X > xMax {
						xMax = pt.X
					}
				}
			}
			var filteredPts []PlotPoint
			for _, pt := range ac.theorySampledSeries.Points {
				if pt.X >= xMin && pt.X <= xMax {
					filteredPts = append(filteredPts, pt)
				}
			}
			if len(filteredPts) > 0 {
				allSeries = append(allSeries, PlotSeries{
					Points:        filteredPts,
					Color:         ac.theorySampledSeries.Color,
					Name:          ac.theorySampledSeries.Name,
					ScatterOnly:   ac.theorySampledSeries.ScatterOnly,
					ScatterRadius: ac.theorySampledSeries.ScatterRadius,
				})
			}
		}

		// DEBUG: Add correlated-noise trend series if available (gated by showDiagnostics).
		if ac.trendSeries != nil {
			var trendPoints []PlotPoint
			for _, pt := range ac.trendSeries.Points {
				if pt.Index < 0 || pt.Index >= len(loadedLightCurveData.FrameNumbers) {
					continue
				}
				frameNum := loadedLightCurveData.FrameNumbers[pt.Index]
				if ac.frameRangeStart > 0 && frameNum < ac.frameRangeStart {
					continue
				}
				if ac.frameRangeEnd > 0 && frameNum > ac.frameRangeEnd {
					continue
				}
				xVal := loadedLightCurveData.TimeValues[pt.Index]
				if useFrameNumbers {
					xVal = frameNum
				}
				trendPoints = append(trendPoints, PlotPoint{
					X:      xVal,
					Y:      pt.Y,
					Index:  pt.Index,
					Series: len(allSeries),
				})
			}
			if len(trendPoints) > 0 {
				allSeries = append(allSeries, PlotSeries{
					Points:   trendPoints,
					Color:    ac.trendSeries.Color,
					Name:     ac.trendSeries.Name,
					LineOnly: ac.trendSeries.LineOnly,
				})
			}
		}

		// apply sets the series on the plot and runs any afterApply callbacks.
		apply := func() {
			if myGen != rebuildGeneration {
				return // a newer rebuildPlot call supersedes this one
			}
			if len(allSeries) == 0 {
				ac.lightCurvePlot.SetSeries(nil)
				plotStatusLabel.SetText("No light curves selected")
			} else {
				ac.lightCurvePlot.SetSeries(allSeries)
				plotStatusLabel.SetText(fmt.Sprintf("Displaying: %s", strings.Join(displayedNames, ", ")))
			}
			for _, fn := range afterApply {
				if fn != nil {
					fn()
				}
			}
			updateRangeEntries()
		}

		// Count total points to decide whether to show a busy dialog.
		totalPoints := 0
		for _, s := range allSeries {
			totalPoints += len(s.Points)
		}

		showBusy := ac.suppressBusyDialog
		ac.suppressBusyDialog = false // consume the flag

		const busyThreshold = 500
		if !showBusy && totalPoints > busyThreshold {
			// Hide any dialog from a previous async rebuild.
			if busyDialog != nil {
				busyDialog.Hide()
				busyDialog = nil
			}
			d := dialog.NewCustomWithoutButtons("", widget.NewLabel("  Redrawing plot — please wait...  "), ac.window)
			d.Show()
			busyDialog = d
			ac.lightCurvePlot.onRenderComplete = func() {
				fyne.Do(func() {
					if busyDialog == d {
						d.Hide()
						busyDialog = nil
					}
				})
			}
			// Defer apply so the dialog frame renders before the heavy plot draw.
			go func() {
				time.Sleep(100 * time.Millisecond)
				fyne.Do(apply)
			}()
		} else {
			apply()
		}
	}

	// Function to toggle a light curve on/off
	ac.toggleLightCurve = func(columnIndex int) {
		if loadedLightCurveData == nil || columnIndex < 0 || columnIndex >= len(loadedLightCurveData.Columns) {
			return
		}

		curveName := loadedLightCurveData.Columns[columnIndex].Name
		if ac.displayedCurves[columnIndex] {
			delete(ac.displayedCurves, columnIndex)
			logAction(fmt.Sprintf("Hid light curve: %s", curveName))
		} else {
			ac.displayedCurves[columnIndex] = true
			logAction(fmt.Sprintf("Displayed light curve: %s", curveName))
		}

		// Reset the Fit tab so analysis starts fresh for the new selection.
		if ac.resetFitTab != nil {
			ac.resetFitTab()
		}

		// Update list bold/non-bold indicators before the heavy plot rebuild.
		lightCurveList.Refresh()

		// Save bounds if the user has set them manually
		var savedMinX, savedMaxX, savedMinY, savedMaxY float64
		if userSetBounds {
			savedMinX, savedMaxX = ac.lightCurvePlot.GetXBounds()
			savedMinY, savedMaxY = ac.lightCurvePlot.GetYBounds()
		}

		ac.rebuildPlot(func() {
			// Restore bounds if the user had set them, otherwise set Y min to 0 and update entries
			if userSetBounds {
				ac.lightCurvePlot.SetXBounds(savedMinX, savedMaxX)
				ac.lightCurvePlot.SetYBounds(savedMinY, savedMaxY)
			} else {
				// Set Y min to 0.0 by default
				_, maxY := ac.lightCurvePlot.GetYBounds()
				ac.lightCurvePlot.SetYBounds(0.0, maxY)
				updateRangeEntries()
			}
		})
	}

	lightCurveList = widget.NewList(
		func() int { return len(lightCurveListData) },
		func() fyne.CanvasObject {
			swatch := canvas.NewRectangle(color.Transparent)
			swatch.SetMinSize(fyne.NewSize(14, 14))
			swatch.CornerRadius = 3
			label := widget.NewLabel("Light Curve Name")
			return container.NewHBox(swatch, label)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			box := obj.(*fyne.Container)
			swatch := box.Objects[0].(*canvas.Rectangle)
			label := box.Objects[1].(*widget.Label)

			name := lightCurveListData[id]
			label.SetText(name)

			// Map list index to actual column index for checking display status
			colIdx := -1
			if id >= 0 && id < len(listIndexToColumnIndex) {
				colIdx = listIndexToColumnIndex[id]
			}
			if ac.displayedCurves[colIdx] {
				// Compute color index matching rebuildPlot order:
				// count displayed curves with column index < colIdx
				ci := 0
				for c := 0; c < colIdx; c++ {
					if ac.displayedCurves[c] {
						ci++
					}
				}
				swatch.FillColor = ac.curveColors[ci%len(ac.curveColors)]
				label.TextStyle.Bold = true
			} else {
				swatch.FillColor = color.Transparent
				label.TextStyle.Bold = false
			}
			swatch.Refresh()
			label.Refresh()
		},
	)

	// Handle click on list items to toggle the display
	lightCurveList.OnSelected = func(id widget.ListItemID) {
		lightCurveList.UnselectAll() // Clear selection highlight immediately
		// Map list index to the actual column index
		if id >= 0 && id < len(listIndexToColumnIndex) {
			ac.toggleLightCurve(listIndexToColumnIndex[id])
		}
	}

	// Set up the function to refresh the light curve list when filter checkboxes change
	refreshLightCurveFilter = func() {
		if loadedLightCurveData == nil {
			return
		}

		// Clear displayed curves
		for k := range ac.displayedCurves {
			delete(ac.displayedCurves, k)
		}

		// Re-filter the light curve list
		lightCurveListData = nil
		listIndexToColumnIndex = nil

		for i, col := range loadedLightCurveData.Columns {
			// If "any name" is checked, include all columns
			if acceptAnyName {
				lightCurveListData = append(lightCurveListData, col.Name)
				listIndexToColumnIndex = append(listIndexToColumnIndex, i)
				continue
			}
			// Check if the column name starts with any enabled prefix
			for prefix, enabled := range lightCurvePrefixes {
				if enabled && strings.HasPrefix(col.Name, prefix) {
					lightCurveListData = append(lightCurveListData, col.Name)
					listIndexToColumnIndex = append(listIndexToColumnIndex, i)
					break
				}
			}
		}
		lightCurveList.UnselectAll()
		lightCurveList.Refresh()

		// Automatically display the first light curve if available
		if len(listIndexToColumnIndex) > 0 {
			ac.toggleLightCurve(listIndexToColumnIndex[0])
		} else {
			ac.rebuildPlot() // Rebuild with an empty plot if no curves match
		}

		logAction("Filter settings changed, refreshed light curve list")
	}

	// Handle start frame entry changes
	ac.startFrameEntry.OnSubmitted = func(text string) {
		val, err := strconv.ParseFloat(text, 64)
		if err != nil {
			ac.startFrameEntry.SetText(fmt.Sprintf("%.0f", ac.frameRangeStart))
			return
		}
		// Validate: not less than ac.minFrameNum
		if val < ac.minFrameNum {
			val = ac.minFrameNum
			ac.startFrameEntry.SetText(fmt.Sprintf("%.0f", val))
		}
		// Validate: end - start must be at least 3
		if ac.frameRangeEnd-val < 3 {
			val = ac.frameRangeEnd - 3
			if val < ac.minFrameNum {
				val = ac.minFrameNum
			}
			ac.startFrameEntry.SetText(fmt.Sprintf("%.0f", val))
		}
		if val != ac.frameRangeStart {
			ac.frameRangeStart = val
			logAction(fmt.Sprintf("Set start frame to %.0f", val))
			// Save Y bounds before rebuild to preserve Y axis scaling
			savedMinY, savedMaxY := ac.lightCurvePlot.GetYBounds()
			ac.rebuildPlot(func() {
				ac.lightCurvePlot.SetYBounds(savedMinY, savedMaxY)
			})
		}
	}

	// Handle end frame entry changes
	ac.endFrameEntry.OnSubmitted = func(text string) {
		val, err := strconv.ParseFloat(text, 64)
		if err != nil {
			ac.endFrameEntry.SetText(fmt.Sprintf("%.0f", ac.frameRangeEnd))
			return
		}
		// Validate: not greater than ac.maxFrameNum
		if val > ac.maxFrameNum {
			val = ac.maxFrameNum
			ac.endFrameEntry.SetText(fmt.Sprintf("%.0f", val))
		}
		// Validate: end - start must be at least 3
		if val-ac.frameRangeStart < 3 {
			val = ac.frameRangeStart + 3
			if val > ac.maxFrameNum {
				val = ac.maxFrameNum
			}
			ac.endFrameEntry.SetText(fmt.Sprintf("%.0f", val))
		}
		if val != ac.frameRangeEnd {
			ac.frameRangeEnd = val
			logAction(fmt.Sprintf("Set end frame to %.0f", val))
			// Save Y bounds before rebuild to preserve Y axis scaling
			savedMinY, savedMaxY := ac.lightCurvePlot.GetYBounds()
			ac.rebuildPlot(func() {
				ac.lightCurvePlot.SetYBounds(savedMinY, savedMaxY)
			})
		}
	}

	// Set the Set trim button callback now that ac.lightCurvePlot, ac.frameRangeStart/End, and ac.rebuildPlot exist
	setTrimBtn.OnTapped = func() {
		var frame1, frame2 float64
		pointsSelected := false

		if ac.lightCurvePlot.MultiPairSelectMode {
			// On the Fit page, two clicks save a PointPair rather than setting
			// SelectedPoint1/2Valid. Use the most recently saved pair for trim.
			if !ac.lightCurvePlot.SelectedPoint1Valid && len(ac.lightCurvePlot.SelectedPairs) > 0 {
				lastPair := ac.lightCurvePlot.SelectedPairs[len(ac.lightCurvePlot.SelectedPairs)-1]
				idx1 := lastPair.Point1DataIdx
				idx2 := lastPair.Point2DataIdx
				if idx1 < 0 || idx2 < 0 {
					dialog.ShowError(fmt.Errorf("both selected points must be data points (not theory curve points)"), w)
					return
				}
				ac.lightCurvePlot.SelectedPairs = ac.lightCurvePlot.SelectedPairs[:len(ac.lightCurvePlot.SelectedPairs)-1]
				if loadedLightCurveData != nil && idx1 < len(loadedLightCurveData.FrameNumbers) {
					frame1 = loadedLightCurveData.FrameNumbers[idx1]
				} else {
					frame1 = float64(idx1)
				}
				if loadedLightCurveData != nil && idx2 < len(loadedLightCurveData.FrameNumbers) {
					frame2 = loadedLightCurveData.FrameNumbers[idx2]
				} else {
					frame2 = float64(idx2)
				}
				pointsSelected = true
			}
		} else {
			if ac.lightCurvePlot.SelectedPoint1Valid && ac.lightCurvePlot.SelectedPoint2Valid {
				idx1 := ac.lightCurvePlot.selectedPointDataIndex
				idx2 := ac.lightCurvePlot.selectedPointDataIndex2
				if idx1 < 0 || idx2 < 0 {
					dialog.ShowError(fmt.Errorf("both selected points must be data points (not theory curve points)"), w)
					return
				}
				if loadedLightCurveData != nil && idx1 < len(loadedLightCurveData.FrameNumbers) {
					frame1 = loadedLightCurveData.FrameNumbers[idx1]
				} else {
					frame1 = float64(idx1)
				}
				if loadedLightCurveData != nil && idx2 < len(loadedLightCurveData.FrameNumbers) {
					frame2 = loadedLightCurveData.FrameNumbers[idx2]
				} else {
					frame2 = float64(idx2)
				}
				pointsSelected = true
			}
		}

		// Fall back to trim entry box values if no points were selected
		if !pointsSelected {
			trimStartText := strings.TrimSpace(trimStartEntry.Text)
			trimEndText := strings.TrimSpace(trimEndEntry.Text)
			if trimStartText == "" || trimEndText == "" {
				dialog.ShowError(fmt.Errorf("select two points on the plot or enter values in the Trim start and Trim end boxes"), w)
				return
			}
			var err error
			frame1, err = strconv.ParseFloat(trimStartText, 64)
			if err != nil {
				dialog.ShowError(fmt.Errorf("invalid Trim start value: %s", trimStartText), w)
				return
			}
			frame2, err = strconv.ParseFloat(trimEndText, 64)
			if err != nil {
				dialog.ShowError(fmt.Errorf("invalid Trim end value: %s", trimEndText), w)
				return
			}
		}

		// Put the smaller frame in Trim start, larger in the Trim end
		if frame1 > frame2 {
			frame1, frame2 = frame2, frame1
		}
		trimStartEntry.SetText(fmt.Sprintf("%.0f", frame1))
		trimEndEntry.SetText(fmt.Sprintf("%.0f", frame2))
		ac.startFrameEntry.SetText(fmt.Sprintf("%.0f", frame1))
		ac.endFrameEntry.SetText(fmt.Sprintf("%.0f", frame2))

		// Update the plot display range
		ac.frameRangeStart = frame1
		ac.frameRangeEnd = frame2
		savedMinY, savedMaxY := ac.lightCurvePlot.GetYBounds()
		ac.rebuildPlot(func() {
			ac.lightCurvePlot.SetYBounds(savedMinY, savedMaxY)

			// Clear selected points
			ac.lightCurvePlot.selectedSeries = -1
			ac.lightCurvePlot.selectedIndex = -1
			ac.lightCurvePlot.selectedPointDataIndex = -1
			ac.lightCurvePlot.selectedSeriesName = ""
			ac.lightCurvePlot.SelectedPoint1Valid = false
			ac.lightCurvePlot.SelectedPoint1Frame = 0
			ac.lightCurvePlot.SelectedPoint1Value = 0
			ac.lightCurvePlot.selectedSeries2 = -1
			ac.lightCurvePlot.selectedIndex2 = -1
			ac.lightCurvePlot.selectedPointDataIndex2 = -1
			ac.lightCurvePlot.selectedSeriesName2 = ""
			ac.lightCurvePlot.SelectedPoint2Valid = false
			ac.lightCurvePlot.SelectedPoint2Frame = 0
			ac.lightCurvePlot.SelectedPoint2Value = 0
			ac.lightCurvePlot.Refresh()
			plotStatusLabel.SetText("Click on a point to see details")
		})

		logAction(fmt.Sprintf("Set trim range: %.0f to %.0f", frame1, frame2))
		trimPerformed = true
		setTrimBtn.Importance = widget.WarningImportance
		setTrimBtn.Refresh()
	}

	// Set the Apply trim button callback
	applyTrimBtn.OnTapped = func() {
		trimStartText := strings.TrimSpace(trimStartEntry.Text)
		trimEndText := strings.TrimSpace(trimEndEntry.Text)
		if trimStartText == "" || trimEndText == "" {
			dialog.ShowError(fmt.Errorf("trim start and trim end values must be set first"), w)
			return
		}
		startVal, err := strconv.ParseFloat(trimStartText, 64)
		if err != nil {
			dialog.ShowError(fmt.Errorf("invalid Trim start value: %v", err), w)
			return
		}
		endVal, err := strconv.ParseFloat(trimEndText, 64)
		if err != nil {
			dialog.ShowError(fmt.Errorf("invalid Trim end value: %v", err), w)
			return
		}
		ac.frameRangeStart = startVal
		ac.frameRangeEnd = endVal
		ac.startFrameEntry.SetText(fmt.Sprintf("%.0f", startVal))
		ac.endFrameEntry.SetText(fmt.Sprintf("%.0f", endVal))
		savedMinY, savedMaxY := ac.lightCurvePlot.GetYBounds()
		ac.rebuildPlot(func() {
			ac.lightCurvePlot.SetYBounds(savedMinY, savedMaxY)
		})
		logAction(fmt.Sprintf("Applied trim: %.0f to %.0f", startVal, endVal))
	}

	// Set the Show all button callback
	showAllBtn.OnTapped = func() {
		if ac.minFrameNum == 0 && ac.maxFrameNum == 0 {
			return
		}
		ac.frameRangeStart = ac.minFrameNum
		ac.frameRangeEnd = ac.maxFrameNum
		ac.startFrameEntry.SetText(fmt.Sprintf("%.0f", ac.minFrameNum))
		ac.endFrameEntry.SetText(fmt.Sprintf("%.0f", ac.maxFrameNum))
		ac.rebuildPlot()
		logAction("Show all: reset frame range to full extent")
	}

	// Right-click on the plot shows all
	ac.lightCurvePlot.SetOnSecondaryTapped(func() {
		showAllBtn.OnTapped()
	})

	// Set the occultation title on the main plot from the last diffraction run
	ac.lightCurvePlot.occultationTitle = lastDiffractionTitle

	// Set up a warning callback for the plot
	ac.lightCurvePlot.SetOnWarning(func(message string) {
		dialog.ShowInformation("Warning", message, w)
	})

	// Set up scroll wheel zoom on the plot with a debounced re-draw
	var scrollDebounceTimer *time.Timer
	ac.lightCurvePlot.SetOnScroll(func(position fyne.Position, scrollDelta float32) {
		if loadedLightCurveData == nil || ac.maxFrameNum == ac.minFrameNum {
			return
		}

		// Get plot size
		plotSize := ac.lightCurvePlot.Size()
		if plotSize.Width <= 0 || plotSize.Height <= 0 {
			return
		}

		// Calculate the relative X position within the plot area (0 to 1)
		// Account for margins
		plotAreaWidth := plotSize.Width - ac.lightCurvePlot.marginLeft - ac.lightCurvePlot.marginRight
		relX := float64((position.X - ac.lightCurvePlot.marginLeft) / plotAreaWidth)

		// Track if the mouse is at/beyond the edges of the plot area
		// Use a small threshold (5%) to make it easier to anchor at edges
		mouseLeftOfPlot := relX <= 0.05
		mouseRightOfPlot := relX >= 0.95

		if relX < 0 {
			relX = 0
		}
		if relX > 1 {
			relX = 1
		}

		// Calculate the frame number under the cursor
		currentRange := ac.frameRangeEnd - ac.frameRangeStart
		frameUnderCursor := ac.frameRangeStart + relX*currentRange

		// Determine zoom factor (scroll up = zoom in, scroll down = zoom out)
		zoomFactor := 1.0
		if scrollDelta > 0 {
			zoomFactor = 0.8 // Zoom in - reduce the range by 20%
		} else if scrollDelta < 0 {
			zoomFactor = 1.25 // Zoom out - increase range by 25%
		} else {
			return
		}

		// Calculate new range
		newRange := currentRange * zoomFactor

		// Ensure a minimum range of 3
		if newRange < 3 {
			newRange = 3
		}

		// Ensure we don't exceed the full data range
		fullRange := ac.maxFrameNum - ac.minFrameNum
		if newRange > fullRange {
			newRange = fullRange
		}

		var newStart, newEnd float64

		if mouseLeftOfPlot {
			// Mouse is to the left of the plot - anchor at the start, only adjust the end
			newStart = ac.frameRangeStart
			newEnd = ac.frameRangeStart + newRange
			// Clamp end only, keep the start anchored
			if newEnd > ac.maxFrameNum {
				newEnd = ac.maxFrameNum
			}
		} else if mouseRightOfPlot {
			// Mouse is to the right of the plot - anchor at the end, only adjust start
			newEnd = ac.frameRangeEnd
			newStart = ac.frameRangeEnd - newRange
			// Clamp start only, keep the end anchored
			if newStart < ac.minFrameNum {
				newStart = ac.minFrameNum
			}
		} else {
			// Mouse is within the plot - keep frameUnderCursor at the same relative position
			newStart = frameUnderCursor - relX*newRange
			newEnd = frameUnderCursor + (1-relX)*newRange

			// Clamp to valid bounds
			if newStart < ac.minFrameNum {
				newStart = ac.minFrameNum
				newEnd = newStart + newRange
			}
			if newEnd > ac.maxFrameNum {
				newEnd = ac.maxFrameNum
				newStart = newEnd - newRange
				if newStart < ac.minFrameNum {
					newStart = ac.minFrameNum
				}
			}
		}

		// Update frame range and UI
		if newStart != ac.frameRangeStart || newEnd != ac.frameRangeEnd {
			ac.frameRangeStart = newStart
			ac.frameRangeEnd = newEnd
			ac.startFrameEntry.SetText(fmt.Sprintf("%.0f", ac.frameRangeStart))
			ac.endFrameEntry.SetText(fmt.Sprintf("%.0f", ac.frameRangeEnd))

			// Debounce: reset the timer on each scroll event so we only
			// rebuild the plot once the scroll wheel has stopped.
			if scrollDebounceTimer != nil {
				scrollDebounceTimer.Stop()
			}
			scrollDebounceTimer = time.AfterFunc(150*time.Millisecond, func() {
				fyne.Do(func() {
					savedMinY, savedMaxY := ac.lightCurvePlot.GetYBounds()
					ac.suppressBusyDialog = true
					ac.rebuildPlot(func() {
						ac.lightCurvePlot.SetYBounds(savedMinY, savedMaxY)
					})
				})
			})
		}
	})

	// Create VizieR tab early so it can be populated from RAVF headers during a file load
	vizierTab = NewVizieRTab()
	// Register vizier tab background for dark mode toggling
	ac.tabBgs = append(ac.tabBgs, tabBgEntry{vizierTab.TabBg, color.RGBA{R: 210, G: 220, B: 210, A: 255}, color.RGBA{R: 60, G: 70, B: 60, A: 255}})
	// Pre-fill asteroid number and name from the persisted diffraction title (e.g. "(2731) Cucula")
	if strings.HasPrefix(lastDiffractionTitle, "(") {
		if end := strings.Index(lastDiffractionTitle, ")"); end > 0 {
			if num := strings.TrimSpace(lastDiffractionTitle[1:end]); num != "" {
				vizierTab.SetAsteroidNumber(num)
			}
			if name := strings.TrimSpace(lastDiffractionTitle[end+1:]); name != "" {
				vizierTab.AsteroidNameEntry.SetText(name)
			}
		}
	}
	// Pre-fill UCAC4 star entry from persisted occelmnt XML
	vizierTab.FillStarFromOccelmntXml(lastLoadedOccelmntXml)

	// Track if csv ops tab has been opened for the first time
	csvOpsTabFirstOpen := true

	// ac.resetFitButtons restores all four fit-page action buttons to their default
	// (HighImportance) color. Assigned after the buttons are created below.

	// ac.resetNormalizeBtn restores the Normalize baseline button to blue. Assigned after the button is created.

	// ac.enablePostFitButtons enables Monte Carlo, NIE, and Fill SODIS after a successful fit. Assigned after those buttons are created.

	// ac.resetProcessOccelmntBtn restores the Process occelmnt file button to blue. Assigned after the button is created.

	// ac.enableShowIOTAPlots enables the Show IOTAdiffraction plots button. Assigned after the button is created.

	// Function to open the CSV file dialog
	openCSVDialog := func() {
		showFileOpenWithRecents(w, prefs, "Select OBS folder then select the light curve csv file", storage.NewExtensionFileFilter([]string{".csv"}), prefs.String("obsHomeDir"), func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if reader == nil {
				return // User cancelled
			}
			filePath := reader.URI().Path()
			if cerr := reader.Close(); cerr != nil {
				dialog.ShowError(fmt.Errorf("failed to close file: %w", cerr), w)
			}

			// Parse the CSV file
			base := filepath.Base(filePath)
			data, err := parseLightCurveCSV(filePath)
			if err != nil {
				dialog.ShowError(fmt.Errorf("%s does not appear to be a light curve file. It is lacking a header line that is normally part of a valid light curve file", base), w)
				return
			}

			// Create a -RESULTS folder in the observation folder
			ext := filepath.Ext(base)
			nameWithoutExt := base[:len(base)-len(ext)]
			resultsFolder = filepath.Join(filepath.Dir(filePath), nameWithoutExt+"-RESULTS")
			if err := os.MkdirAll(resultsFolder, 0755); err != nil {
				fmt.Printf("Warning: could not create results folder %s: %v\n", resultsFolder, err)
				resultsFolder = ""
			}

			// Copy the CSV file into the results folder
			if resultsFolder != "" {
				csvDest := filepath.Join(resultsFolder, base)
				if srcBytes, err := os.ReadFile(filePath); err == nil {
					if err := os.WriteFile(csvDest, srcBytes, 0644); err != nil {
						fmt.Printf("Warning: could not copy CSV to results folder: %v\n", err)
					}
				}
			}

			// Check the -RESULTS folder for prior run artifacts (.occparams, .site, targetImage16bit.png).
			// If all three are present, restore the state so the Fit tab can be used immediately.
			var foundOccparams, foundSite, foundImage, foundGeoShadow string
			if resultsFolder != "" {
				if entries, err := os.ReadDir(resultsFolder); err == nil {
					for _, entry := range entries {
						if entry.IsDir() {
							continue
						}
						name := entry.Name()
						switch {
						case strings.ToLower(filepath.Ext(name)) == ".occparams" && foundOccparams == "":
							foundOccparams = filepath.Join(resultsFolder, name)
						case strings.ToLower(filepath.Ext(name)) == ".site" && foundSite == "":
							foundSite = filepath.Join(resultsFolder, name)
						case name == "targetImage16bit.png":
							foundImage = filepath.Join(resultsFolder, name)
						case name == "geometricShadow.png":
							foundGeoShadow = filepath.Join(resultsFolder, name)
						}
					}
				}
				if foundOccparams != "" && foundSite != "" && foundImage != "" {
					// Copy the diffraction images to the application directory
					for _, img := range []struct{ src, name string }{
						{foundImage, "targetImage16bit.png"},
						{foundGeoShadow, "geometricShadow.png"},
					} {
						if img.src == "" {
							continue
						}
						if imgData, err := os.ReadFile(img.src); err == nil {
							dstPath := filepath.Join(appDir, img.name)
							if err := os.WriteFile(dstPath, imgData, 0644); err != nil {
								fmt.Printf("Warning: could not copy %s to app dir: %v\n", img.name, err)
							}
						}
					}

					// Set up the parameters path and title so the Fit tab recognizes a diffraction run
					logOccparamsRead("prior results restore", foundOccparams)
					lastDiffractionParamsPath = foundOccparams
					prefs.SetString("lastDiffractionParamsPath", foundOccparams)
					if f, err := os.Open(foundOccparams); err == nil {
						if p, err := parseOccultationParameters(f); err == nil && p.Title != "" {
							lastDiffractionTitle = normalizeAsteroidTitle(p.Title)
							prefs.SetString("lastDiffractionTitle", lastDiffractionTitle)
							ac.lightCurvePlot.occultationTitle = lastDiffractionTitle
						}
						if cerr := f.Close(); cerr != nil {
							fmt.Printf("Warning: failed to close occparams file: %v\n", cerr)
						}
					}

					// Fill site data from the .site file
					lastLoadedSitePath = foundSite
					if sf, err := os.Open(foundSite); err == nil {
						scanner := bufio.NewScanner(sf)
						for scanner.Scan() {
							line := scanner.Text()
							if strings.HasPrefix(line, "latitude_decimal:") {
								val := strings.TrimSpace(strings.TrimPrefix(line, "latitude_decimal:"))
								if lat, perr := strconv.ParseFloat(val, 64); perr == nil {
									lastObserverLatDeg = lat
									lastObserverLocationSet = true
									prefs.SetFloat("lastObserverLatDeg", lat)
									prefs.SetBool("lastObserverLocationSet", true)
									deg, minutes, sec := decimalToDMS(lat)
									vizierTab.SiteLatDegEntry.SetText(deg)
									vizierTab.SiteLatMinEntry.SetText(minutes)
									vizierTab.SiteLatSecsEntry.SetText(sec)
								}
							} else if strings.HasPrefix(line, "longitude_decimal:") {
								val := strings.TrimSpace(strings.TrimPrefix(line, "longitude_decimal:"))
								if lon, perr := strconv.ParseFloat(val, 64); perr == nil {
									lastObserverLonDeg = lon
									prefs.SetFloat("lastObserverLonDeg", lon)
									deg, minutes, sec := decimalToDMS(lon)
									vizierTab.SiteLongDegEntry.SetText(deg)
									vizierTab.SiteLongMinEntry.SetText(minutes)
									vizierTab.SiteLongSecsEntry.SetText(sec)
								}
							} else if strings.HasPrefix(line, "altitude:") {
								val := strings.TrimSpace(strings.TrimPrefix(line, "altitude:"))
								if alt, perr := strconv.ParseFloat(val, 64); perr == nil {
									lastObserverAltMeters = alt
									prefs.SetFloat("lastObserverAltMeters", alt)
								}
								vizierTab.SiteAltitudeEntry.SetText(val)
							} else if strings.HasPrefix(line, "observer1:") {
								val := strings.TrimSpace(strings.TrimPrefix(line, "observer1:"))
								vizierTab.ObserverNameEntry.SetText(val)
							}
						}
						if cerr := sf.Close(); cerr != nil {
							fmt.Printf("Warning: failed to close site file: %v\n", cerr)
						}
					}
					logAction(fmt.Sprintf("Restored prior results: occparams=%s, site=%s, image copied", foundOccparams, foundSite))
				}
			}

			priorResultsFound := foundOccparams != "" && foundSite != "" && foundImage != ""

			loadedLightCurveData = data
			sodisReportSavedThisSession = false
			vizierDatWrittenThisSession = false
			sodisNegativeReportSaved = false
			occultationProcessedForCurrentCSV = priorResultsFound
			if ac.resetFitButtons != nil {
				ac.resetFitButtons()
			}
			if ac.resetNormalizeBtn != nil {
				ac.resetNormalizeBtn()
			}
			trimPerformed = false
			setTrimBtn.Importance = widget.HighImportance
			setTrimBtn.Refresh()
			if priorResultsFound {
				// Prior results found: blue button (enabled, no blink).
				if ac.stopProcessOccelmntBlink != nil {
					ac.stopProcessOccelmntBlink()
				}
				if ac.resetProcessOccelmntBtn != nil {
					ac.resetProcessOccelmntBtn()
				}
				// Stop the blink that resetProcessOccelmntBtn just started.
				if ac.stopProcessOccelmntBlink != nil {
					ac.stopProcessOccelmntBlink()
				}
				priorDlg := dialog.NewInformation("Prior Results Found",
					"This observation has been fully processed in the past.\n"+
						"Unless you need to change site data or an occultation parameter value, "+
						"the Fit tab will be opened automatically.", w)
				priorDlg.SetOnClosed(func() {
					if ac.selectFitTab != nil {
						ac.selectFitTab()
					}
				})
				priorDlg.Show()
			} else {
				if ac.resetProcessOccelmntBtn != nil {
					ac.resetProcessOccelmntBtn()
				}
			}
			// Check for camera-timing.txt in the observation directory
			cameraTimingPath := filepath.Join(filepath.Dir(filePath), "camera-timing.txt")
			if ctData, err := os.ReadFile(cameraTimingPath); err == nil {
				for _, line := range strings.Split(string(ctData), "\n") {
					line = strings.TrimSpace(line)
					if k, v, ok := strings.Cut(line, "="); ok {
						switch k {
						case "cameraName":
							sessionCameraName = v
						case "acqDelay":
							sessionAcqDelay = v
						case "starRow":
							sessionStarRow = v
						case "rowDelta":
							sessionRowDelta = v
						}
					}
				}
				if ac.confirmAcqTiming != nil {
					ac.confirmAcqTiming()
				}
			}

			// Create an action log file for this CSV
			if err := createActionLog(filePath); err != nil {
				fmt.Printf("Warning: could not create log file: %v\n", err)
			}
			logAction(fmt.Sprintf("Loaded CSV with %d columns and %d data points", len(data.Columns), len(data.TimeValues)))
			w.SetTitle("GoPyOTE Version: " + Version + " — " + filePath)

			// Check if the timestamp column was empty (all zeros)
			timestampsEmpty := true
			for _, t := range data.TimeValues {
				if t != 0 {
					timestampsEmpty = false
					break
				}
			}
			if timestampsEmpty && len(data.TimeValues) > 0 {
				dialog.ShowInformation("Warning", "Manual timestamping is required.", w)
			}

			// Analyze timing errors if timestamps are available
			resetInterpolatedIndices()  // Clear any previous interpolated indices
			resetNegativeDeltaIndices() // Clear any previous negative delta indices
			if !timestampsEmpty && len(data.TimeValues) > 1 {
				timingResult := analyzeTimingErrors(data.TimeValues)
				if timingResult != nil && (len(timingResult.CadenceErrors) > 0 || len(timingResult.DroppedFrameErrors) > 0 || len(timingResult.NegativeDeltaErrors) > 0) {
					// Fix negative delta timestamps first (before interpolation)
					if len(timingResult.NegativeDeltaErrors) > 0 {
						timingResult.NegativeDeltaFixed = fixNegativeDeltaTimestamps(data, timingResult.NegativeDeltaErrors, timingResult.AverageTimeStep)
						logAction(fmt.Sprintf("Fixed %d negative delta timestamps", timingResult.NegativeDeltaFixed))
					}
					// Interpolate dropped frames
					if len(timingResult.DroppedFrameErrors) > 0 {
						timingResult.InterpolatedCount = interpolateDroppedFrames(data, timingResult.DroppedFrameErrors)
						logAction(fmt.Sprintf("Interpolated %d dropped frames", timingResult.InterpolatedCount))
					}
					// Mark negative delta indices AFTER interpolation, adjusting for inserted points
					if len(timingResult.NegativeDeltaErrors) > 0 {
						for _, negErr := range timingResult.NegativeDeltaErrors {
							// Calculate how many interpolated points were inserted before this index
							offset := 0
							for _, dropErr := range timingResult.DroppedFrameErrors {
								if dropErr.Index <= negErr.Index {
									offset += dropErr.DroppedCount
								}
							}
							markNegativeDeltaIndex(negErr.Index + offset)
						}
					}
					// Show timing error report dialog
					report := formatTimingReport(timingResult)
					logAction(fmt.Sprintf("Timing analysis: %d cadence errors, %d dropped frame gaps, %d negative deltas",
						len(timingResult.CadenceErrors), len(timingResult.DroppedFrameErrors), len(timingResult.NegativeDeltaErrors)))
					dialog.ShowInformation("Timing Analysis", report, w)
				}

				if timingResult != nil {
					lastCsvExposureSecs = timingResult.MedianTimeStep
				}
			}

			// Clear displayed curves and reset the plot
			for k := range ac.displayedCurves {
				delete(ac.displayedCurves, k)
			}
			ac.lightCurvePlot.SetSeries(nil)
			ac.lightCurvePlot.occultationTitle = filepath.Base(filePath) // Use CSV filename as a plot title
			ac.smoothedSeries = nil                                      // Clear any previous smooth curve
			normalizationApplied = false                                 // Reset normalization flag
			baselineScaledToUnity = false
			ac.theorySeries = nil
			ac.theorySampledSeries = nil
			ac.trendSeries = nil
			ac.lightCurvePlot.SetVerticalLines(nil, false)
			ac.lightCurvePlot.SetSigmaLines(nil, false)
			ac.lightCurvePlot.ShowBaselineLine = false

			// Clear range entries and reset the user bounds flag
			userSetBounds = false
			yMinEntry.SetText("")
			yMaxEntry.SetText("")

			// Update the list with column names, filtered by selected prefixes
			lightCurveListData = nil
			listIndexToColumnIndex = nil
			for i, col := range data.Columns {
				// If "any name" is checked, include all columns
				if acceptAnyName {
					lightCurveListData = append(lightCurveListData, col.Name)
					listIndexToColumnIndex = append(listIndexToColumnIndex, i)
					continue
				}
				// Check if the column name starts with any enabled prefix
				for prefix, enabled := range lightCurvePrefixes {
					if enabled && strings.HasPrefix(col.Name, prefix) {
						lightCurveListData = append(lightCurveListData, col.Name)
						listIndexToColumnIndex = append(listIndexToColumnIndex, i)
						break
					}
				}
			}
			lightCurveList.UnselectAll()
			lightCurveList.Refresh()

			// Initialize frame number range entries and variables BEFORE displaying curves
			if len(data.FrameNumbers) > 0 {
				ac.minFrameNum = data.FrameNumbers[0]
				ac.maxFrameNum = data.FrameNumbers[len(data.FrameNumbers)-1]
				ac.frameRangeStart = ac.minFrameNum
				ac.frameRangeEnd = ac.maxFrameNum
				ac.startFrameEntry.SetText(fmt.Sprintf("%.0f", ac.frameRangeStart))
				ac.endFrameEntry.SetText(fmt.Sprintf("%.0f", ac.frameRangeEnd))
			} else {
				// Reset frame range if no frame numbers
				ac.frameRangeStart = 0
				ac.frameRangeEnd = 0
			}

			// Automatically display the first light curve if available
			if len(listIndexToColumnIndex) > 0 {
				ac.toggleLightCurve(listIndexToColumnIndex[0])
			}

			plotStatusLabel.SetText("Click on a point to see details")

			// Clear VizieR fields, then populate from RAVF or ADV headers if applicable
			vizierTab.ClearInputs()
			vizierTab.FillFromRavfHeaders(data.SkippedLines)
			vizierTab.FillFromAdvHeaders(data.SkippedLines)
		})
	}

	// Button to load a CSV file
	loadCSVBtn := widget.NewButton("Open browser to select csv file", func() {
		openCSVDialog()
	})

	lightCurveListScroll := container.NewVScroll(lightCurveList)
	lightCurveListScroll.SetMinSize(fyne.NewSize(200, 300))

	// Button to export selected light curves to CSV
	exportCSVBtn := widget.NewButton("Export selected light curves", func() {
		if loadedLightCurveData == nil {
			dialog.ShowError(fmt.Errorf("no light curve data loaded"), w)
			return
		}
		if len(ac.displayedCurves) == 0 {
			dialog.ShowError(fmt.Errorf("no light curves selected"), w)
			return
		}

		outputPath, err := writeSelectedLightCurves(loadedLightCurveData, ac.displayedCurves, ac.frameRangeStart, ac.frameRangeEnd)
		if err != nil {
			dialog.ShowError(err, w)
			logAction(fmt.Sprintf("Export failed: %v", err))
			return
		}

		logAction(fmt.Sprintf("Exported selected light curves to: %s", outputPath))
		dialog.ShowInformation("Export Complete",
			fmt.Sprintf("Selected light curves exported to:\n%s", outputPath), w)
	})

	// Buttons row for Data tab - wrap in containers with minimum size for full labels
	loadCSVBtnContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(220, 36)), loadCSVBtn)
	exportCSVBtnContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(220, 36)), exportCSVBtn)
	dataTabButtons := container.NewHBox(loadCSVBtnContainer, exportCSVBtnContainer)

	tab3Content := container.NewStack(tab3Bg, container.NewPadded(container.NewBorder(
		dataTabButtons,       // top
		nil,                  // bottom (frame controls moved to plot area)
		nil,                  // left
		nil,                  // right
		lightCurveListScroll, // center
	)))
	tab3 := container.NewTabItem("OBS select", tab3Content)

	tab5 := buildBlockIntTab(ac)

	tab6 := buildFlashTagsTab(ac)

	tab7 := buildSmoothTab(ac)

	// Tab 8: VizieR export - vizierTab was created earlier to allow RAVF header parsing during file load
	// Set up Generate button callback (needs access to local variables)
	vizierTab.GenerateBtn.OnTapped = func() {
		// Validate inputs
		year, month, day, err := vizierTab.ValidateInputs(w)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}

		// Check for loaded data
		if loadedLightCurveData == nil {
			dialog.ShowError(fmt.Errorf("no light curve data loaded"), w)
			return
		}

		// Get formatted values
		hipparcos, tycho2, ucac4 := vizierTab.GetFormattedStarIDs()
		longDeg, longMin, longSecs, latDeg, latMin, latSecs, altitude := vizierTab.GetFormattedLocation()

		// Get min/max frame numbers directly from loaded data
		dataMinFrame := loadedLightCurveData.FrameNumbers[0]
		dataMaxFrame := loadedLightCurveData.FrameNumbers[len(loadedLightCurveData.FrameNumbers)-1]

		// Check if we need to use a frame range
		rangeStart := int(ac.frameRangeStart)
		rangeEnd := int(ac.frameRangeEnd)
		if rangeStart == 0 {
			rangeStart = int(dataMinFrame)
		}
		if rangeEnd == 0 {
			rangeEnd = int(dataMaxFrame)
		}

		// Check if data is not trimmed and warn the user
		if rangeStart == int(dataMinFrame) && rangeEnd == int(dataMaxFrame) {
			dialog.ShowConfirm("Have you trimmed the light curve?",
				"Only the occultation event and enough points on either side\n"+
					"of the event to allow baseline noise to be well represented\n"+
					"are needed. Typically around a hundred points on either side\n"+
					"will be sufficient.\n\n"+
					"Do you wish to write the light curve as is?",
				func(proceed bool) {
					if !proceed {
						return
					}
					generateVizieRFile(w, loadedLightCurveData, year, month, day,
						hipparcos, tycho2, ucac4,
						longDeg, longMin, longSecs,
						latDeg, latMin, latSecs,
						altitude, vizierTab.ObserverNameEntry.Text,
						vizierTab.AsteroidNumberEntry.Text, vizierTab.AsteroidNameEntry.Text,
						rangeStart, rangeEnd,
						vizierTab.StatusLabel)
				}, w)
			return
		}

		generateVizieRFile(w, loadedLightCurveData, year, month, day,
			hipparcos, tycho2, ucac4,
			longDeg, longMin, longSecs,
			latDeg, latMin, latSecs,
			altitude, vizierTab.ObserverNameEntry.Text,
			vizierTab.AsteroidNumberEntry.Text, vizierTab.AsteroidNameEntry.Text,
			rangeStart, rangeEnd,
			vizierTab.StatusLabel)
	}

	// Set up the Preview submission button callback
	vizierTab.PreviewBtn.OnTapped = func() {
		// Validate all fields - must be complete before preview/generate.
		if _, _, _, err := vizierTab.ValidateInputs(w); err != nil {
			dialog.ShowError(err, w)
			return
		}

		// Check for loaded data.
		if loadedLightCurveData == nil {
			dialog.ShowError(fmt.Errorf("no light curve data loaded"), w)
			return
		}

		// Determine the active frame range.
		dataMinFrame := loadedLightCurveData.FrameNumbers[0]
		dataMaxFrame := loadedLightCurveData.FrameNumbers[len(loadedLightCurveData.FrameNumbers)-1]
		rangeStart := int(ac.frameRangeStart)
		rangeEnd := int(ac.frameRangeEnd)
		if rangeStart == 0 {
			rangeStart = int(dataMinFrame)
		}
		if rangeEnd == 0 {
			rangeEnd = int(dataMaxFrame)
		}

		// Find start/end indices.
		startIdx := 0
		endIdx := len(loadedLightCurveData.FrameNumbers) - 1
		foundStart := false
		for i, frameNum := range loadedLightCurveData.FrameNumbers {
			if int(frameNum) >= rangeStart && !foundStart {
				startIdx = i
				foundStart = true
			}
			if int(frameNum) <= rangeEnd {
				endIdx = i
			}
		}

		// Find the signal column (same logic as generateVizieRFile).
		var valueColumn []float64
		for _, col := range loadedLightCurveData.Columns {
			if strings.HasPrefix(col.Name, "signal") {
				valueColumn = col.Values
				break
			}
		}
		if valueColumn == nil && len(loadedLightCurveData.Columns) > 0 {
			valueColumn = loadedLightCurveData.Columns[0].Values
		}
		if valueColumn == nil {
			dialog.ShowError(fmt.Errorf("no data columns available"), w)
			return
		}

		// Compute VizieR scale factor (normalise to 0–9524).
		maxValue := 0.0
		for i := startIdx; i <= endIdx; i++ {
			if !isInterpolatedIndex(i) && valueColumn[i] > maxValue {
				maxValue = valueColumn[i]
			}
		}
		scaleFactor := 9524.0 / maxValue
		if maxValue == 0 {
			scaleFactor = 1.0
		}

		// Build per-point slices for the submission range.
		numPts := endIdx - startIdx + 1
		timestamps := make([]float64, numPts)
		scaledValues := make([]int, numPts)
		dropped := make([]bool, numPts)
		for j := 0; j < numPts; j++ {
			idx := startIdx + j
			timestamps[j] = loadedLightCurveData.TimeValues[idx]
			if isInterpolatedIndex(idx) {
				dropped[j] = true
			} else {
				scaledValues[j] = int(valueColumn[idx] * scaleFactor)
			}
		}

		// Build plot title.
		plotTitle := fmt.Sprintf("VizieR Preview — Asteroid %s (%s)",
			vizierTab.AsteroidNumberEntry.Text, vizierTab.AsteroidNameEntry.Text)

		// Create the scaled light-curve plot.
		plotImg, err := createVizieRPreviewPlotImage(timestamps, scaledValues, dropped, plotTitle, 1100, 500)
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to create preview plot: %w", err), w)
			return
		}

		// Save the preview plot PNG to the results' folder.
		if resultsFolder != "" {
			var pngBuf bytes.Buffer
			if encErr := png.Encode(&pngBuf, plotImg); encErr == nil {
				savePath := filepath.Join(resultsFolder, "vizierPreviewPlot.png")
				if werr := os.WriteFile(savePath, pngBuf.Bytes(), 0644); werr != nil {
					fmt.Printf("Warning: could not save VizieR preview plot: %v\n", werr)
				} else {
					logAction("Saved VizieR preview plot: " + savePath)
				}
			}
		}

		// Show the plot in a new window.
		previewWin := a.NewWindow("VizieR Submission Preview")
		plotCanvas := canvas.NewImageFromImage(plotImg)
		plotCanvas.FillMode = canvas.ImageFillOriginal
		previewWin.SetContent(container.NewScroll(plotCanvas))
		previewWin.Resize(fyne.NewSize(1150, 550))
		previewWin.CenterOnScreen()
		safeShowWindow(previewWin)

		// Enable the Generate button now that the user has previewed the submission.
		vizierTab.GenerateBtn.Enable()
	}

	// Set up Load from NA spreadsheet button callback
	vizierTab.LoadXlsxBtn.OnTapped = func() {
		vizierTab.FillFromNASpreadsheet(w)
	}

	// Set up Load from SODIS form button callback
	vizierTab.LoadSodisBtn.OnTapped = func() {
		vizierTab.FillFromSodisForm(w)
	}

	tab10 := buildFitTab(ac)

	tabs := container.NewAppTabs(tab2, tab3, tab10, vizierTab.TabItem, tab5, tab6, tab7)

	ac.selectFitTab = func() {
		tabs.Select(tab10)
	}
	ac.selectVizierTab = func() {
		tabs.Select(vizierTab.TabItem)
	}

	// Apply dark tab backgrounds if dark mode was persisted
	if prefs.BoolWithFallback("darkMode", false) {
		ac.applyTabBgTheme(true)
	}

	// Handle tab selection events
	tabs.OnSelected = func(tab *container.TabItem) {
		// Track whether Fit tab is active and handle multi-pair selection mode
		previousOnFitTab := onFitTab
		onFitTab = tab == tab10

		// Clear saved pairs and baseline line when leaving the Fit tab
		if previousOnFitTab && !onFitTab {
			ac.lightCurvePlot.SelectedPairs = nil
			ac.lightCurvePlot.ShowBaselineLine = false
			ac.lightCurvePlot.Refresh()
			// Stop Image Acquisition Timing blink and reset the button color
			if ac.stopAcqTimingBlink != nil {
				ac.stopAcqTimingBlink()
			}
		}

		// Set selection modes based on the current tab
		if tab == tab6 {
			// Flash tags tab: enable single select mode
			ac.lightCurvePlot.SingleSelectMode = true
			ac.lightCurvePlot.MultiPairSelectMode = false
			// Clear point 2 selection when entering the Flash tags tab
			ac.lightCurvePlot.selectedSeries2 = -1
			ac.lightCurvePlot.selectedIndex2 = -1
			ac.lightCurvePlot.selectedPointDataIndex2 = -1
			ac.lightCurvePlot.selectedSeriesName2 = ""
			ac.lightCurvePlot.SelectedPoint2Valid = false
			ac.lightCurvePlot.SelectedPoint2Frame = 0
			ac.lightCurvePlot.SelectedPoint2Value = 0
			ac.lightCurvePlot.Refresh()
		} else if tab == tab10 {
			// Fit tab: require that the occultation has been processed first
			if !occultationProcessedForCurrentCSV {
				dialog.ShowError(fmt.Errorf(
					"The \"Process occelmnt file\" operation must be performed first.\n\n"+
						"Please process an occultation file before using the Fit tab."), w)
				tabs.Select(tab3)
				return
			}
			// Fit tab: require exactly one light curve to be displayed
			if len(ac.displayedCurves) != 1 {
				dialog.ShowError(fmt.Errorf(
					"The Fit page requires a CSV file to be loaded with exactly one light curve displayed.\n\n"+
						"Currently %d light curve(s) are selected.\n\n"+
						"Please load a CSV file and select a single light curve before opening the Fit page.",
					len(ac.displayedCurves)), w)
				tabs.Select(tab3)
				return
			}

			// Fit tab: multi-pair mode for baseline selection, unless the NIE manual
			// selection checkbox is checked, in which case keep two-point mode.
			ac.lightCurvePlot.SingleSelectMode = false
			ac.lightCurvePlot.MultiPairSelectMode = !ac.nieManualSelectMode
			ac.lightCurvePlot.Refresh()

			// Autofill search range defaults from the parameters file only when
			// the entries are still empty (first visit).
			if ac.autoFillSearchRange != nil {
				ac.autoFillSearchRange()
			}

			// Flash the Image Acquisition Timing button if the user has
			// previously set values but hasn't confirmed them this session.
			if ac.startAcqTimingBlink != nil {
				ac.startAcqTimingBlink()
			}
		} else {
			ac.lightCurvePlot.SingleSelectMode = false
			ac.lightCurvePlot.MultiPairSelectMode = false
		}

		// csv ops tab: open the file dialog on the first visit
		if tab == tab3 && csvOpsTabFirstOpen {
			csvOpsTabFirstOpen = false
			openCSVDialog()
		}

		// VizieR tab: check that exactly one light curve is selected
		if tab == vizierTab.TabItem {
			numDisplayed := len(ac.displayedCurves)
			if numDisplayed != 1 {
				dialog.ShowError(fmt.Errorf("a single light curve must be selected for use by the VizieR export function.\n\nCurrently %d curves are selected.\n\nBe sure to set the Start Frame and End Frame values so as to trim the data points sent to VizieR to be no more than about 100 points surrounding the event (if possible).", numDisplayed), w)
				tabs.Select(tab3) // Switch to the.csv ops tab
			}
		}
	}

	// Select the OBS select tab on startup
	tabs.Select(tab3)

	// Shared widgets for the IOTAdiffraction output window.
	// A fresh OS window is created each time to let Fyne center it automatically.
	iotaOutputLabel := widget.NewLabel("Starting IOTAdiffraction...")
	iotaOutputLabel.Wrapping = fyne.TextWrapWord
	iotaScrollContainer := container.NewVScroll(container.NewPadded(iotaOutputLabel))
	var iotaOutputWindow fyne.Window

	// Helper function to run IOTAdiffraction with a given parameter file
	runIOTAdiffraction := func(paramFilePath string) {
		// This function runs inside a goroutine — all UI updates use fyne.Do.

		// If the parameters file has a non-empty path_to_qe_table_file, prefix it with CAMERA-QE/
		// and write a temporary modified copy for IOTAdiffraction to use.
		actualParamFile := paramFilePath
		var tempParamFile string
		if content, err := os.ReadFile(paramFilePath); err == nil {
			var params OccultationParameters
			if err := json5.Unmarshal(content, &params); err == nil && params.PathToQeTableFile != "" {
				if !strings.HasPrefix(params.PathToQeTableFile, "CAMERA-QE/") && !strings.HasPrefix(params.PathToQeTableFile, "CAMERA-QE\\") {
					prefixed := "CAMERA-QE/" + params.PathToQeTableFile
					// Replace the value in the raw file content to preserve JSON5 formatting
					modified := strings.Replace(string(content),
						fmt.Sprintf("%q", params.PathToQeTableFile),
						fmt.Sprintf("%q", prefixed), 1)
					tmpFile, err := os.CreateTemp(appDir, "iotadiff-*.occparams")
					if err == nil {
						if _, err := tmpFile.WriteString(modified); err == nil {
							tempParamFile = tmpFile.Name()
							actualParamFile = tempParamFile
							logAction(fmt.Sprintf("QE file path prefixed: %s -> %s", params.PathToQeTableFile, prefixed))
						}
						if cerr := tmpFile.Close(); cerr != nil {
							fmt.Printf("Warning: failed to close temp params file: %v\n", cerr)
						}
					}
					if tempParamFile != "" {
						logOccparamsWrite("IOTAdiffraction temp file", tempParamFile)
					}
				}
			}
		}

		logOccparamsRead("IOTAdiffraction input", actualParamFile)

		// Build the path to IOTAdiffraction.exe using the app directory
		exePath := filepath.Join(appDir, "IOTAdiffraction.exe")

		// Check if the file exists
		if _, err := os.Stat(exePath); os.IsNotExist(err) {
			iotaDiffractionRunning.Store(false)
			fyne.Do(func() {
				iotaOutputWindow.Hide()
				dialog.ShowInformation("File Not Found",
					"IOTAdiffraction.exe was not found in the application directory.\n\n"+
						"Please ensure the file is located at:\n"+exePath, w)
			})
			return
		}

		// Set up the command with pipes using the selected file as a parameter
		showPlots := fmt.Sprintf("%v", prefs.BoolWithFallback("showIOTAPlots", true))
		cmd := exec.Command(exePath, actualParamFile, showPlots)
		cmd.Dir = appDir
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			iotaDiffractionRunning.Store(false)
			fyne.Do(func() {
				iotaOutputWindow.Hide()
				dialog.ShowError(fmt.Errorf("error creating stdout pipe: %v", err), w)
			})
			return
		}

		stderr, err := cmd.StderrPipe()
		if err != nil {
			iotaDiffractionRunning.Store(false)
			fyne.Do(func() {
				iotaOutputWindow.Hide()
				dialog.ShowError(fmt.Errorf("error creating stderr pipe: %v", err), w)
			})
			return
		}

		// Start the command
		if err := cmd.Start(); err != nil {
			iotaDiffractionRunning.Store(false)
			fyne.Do(func() {
				iotaOutputWindow.Hide()
				dialog.ShowError(fmt.Errorf("error starting IOTAdiffraction: %v", err), w)
			})
			return
		}

		fyne.Do(func() { iotaOutputLabel.SetText("") })

		// Mutex to protect output text updates
		var mu sync.Mutex
		var outputLines string

		appendOutput := func(line string) {
			mu.Lock()
			outputLines += line + "\n"
			text := outputLines
			mu.Unlock()
			fyne.Do(func() {
				iotaOutputLabel.SetText(text)
				iotaScrollContainer.ScrollToBottom()
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
			iotaDiffractionRunning.Store(false)
			logAction("IOTAdiffraction process exited")
			// Clean up the temporary parameter file if one was created
			if tempParamFile != "" {
				if rerr := os.Remove(tempParamFile); rerr != nil {
					fmt.Printf("Warning: could not remove temp params file: %v\n", rerr)
				}
			}
			if err != nil {
				logAction(fmt.Sprintf("IOTAdiffraction failed: %v", err))
				appendOutput(fmt.Sprintf("\n[Error: %v]", err))
			} else {
				logAction("IOTAdiffraction completed successfully")
				// Log image file timestamps immediately after completion
				for _, imgName := range []string{"targetImage16bit.png", "geometricShadow.png"} {
					imgPath := filepath.Join(appDir, imgName)
					if info, serr := os.Stat(imgPath); serr == nil {
						logAction(fmt.Sprintf("  %s: modified %s, %d bytes",
							imgName, info.ModTime().Format("2006-01-02 15:04:05"), info.Size()))
					} else {
						logAction(fmt.Sprintf("  %s: NOT FOUND in %s", imgName, appDir))
					}
				}
				appendOutput("\n[Process completed successfully]")
				if ac.enableShowIOTAPlots != nil {
					fyne.Do(func() { ac.enableShowIOTAPlots() })
				}
				// Copy targetImage16bit.png and geometricShadow.png to the -RESULTS folder if available.
				if resultsFolder != "" {
					for _, imgName := range []string{"targetImage16bit.png", "geometricShadow.png"} {
						srcPath := filepath.Join(appDir, imgName)
						if data, rerr := os.ReadFile(srcPath); rerr == nil {
							dstPath := filepath.Join(resultsFolder, imgName)
							if werr := os.WriteFile(dstPath, data, 0644); werr != nil {
								fmt.Printf("Warning: could not copy %s to results folder: %v\n", imgName, werr)
							} else {
								logAction(fmt.Sprintf("%s copied to: %s", imgName, dstPath))
							}
						}
					}
				}
			}
		}()
	}

	// useParamFile sets up a global state and runs IOTAdiffraction with the given .occparams file.
	// It can be triggered automatically when the parameters dialog saves a new file.
	useParamFile := func(paramFilePath string) {
		logOccparamsRead("useParamFile", paramFilePath)
		iotaDiffractionRunning.Store(true)
		lastDiffractionParamsPath = paramFilePath
		prefs.SetString("lastDiffractionParamsPath", paramFilePath)
		occultationProcessedForCurrentCSV = true

		// Create a fresh output window each time so Fyne centres it on screen.
		iotaOutputLabel.SetText("Starting IOTAdiffraction...")
		if iotaOutputWindow != nil {
			iotaOutputWindow.Close()
		}
		iotaOutputWindow = a.NewWindow("IOTAdiffraction Output")
		iotaOutputWindow.SetContent(iotaScrollContainer)
		iotaOutputWindow.Resize(fyne.NewSize(520, 350))
		safeShowWindow(iotaOutputWindow)

		// Move ALL remaining work (param parsing, VizieR fills, process
		// launch) into a goroutine so the dialog can render immediately.
		go func() {
			// Extract title and embedded occelmnt XML from the parameters file
			title := ""
			var occXml string
			if f, err := os.Open(paramFilePath); err == nil {
				if p, err := parseOccultationParameters(f); err == nil {
					title = normalizeAsteroidTitle(p.Title)
					occXml = p.OccelmntXml
				}
				if err := f.Close(); err != nil {
					fmt.Printf("Warning: failed to close parameters file: %v\n", err)
				}
			}

			// UI updates must happen on the main thread
			fyne.Do(func() {
				lastDiffractionTitle = title
				prefs.SetString("lastDiffractionTitle", lastDiffractionTitle)
				ac.lightCurvePlot.occultationTitle = lastDiffractionTitle
				if occXml != "" {
					lastLoadedOccelmntXml = occXml
					prefs.SetString("lastLoadedOccelmntXml", lastLoadedOccelmntXml)
					vizierTab.FillStarFromOccelmntXml(lastLoadedOccelmntXml)
				}
				// Fill VizieR Number and Name entries from title (e.g. "(2731) Cucula" -> "2731", "Cucula")
				if strings.HasPrefix(lastDiffractionTitle, "(") {
					if end := strings.Index(lastDiffractionTitle, ")"); end > 0 {
						if num := strings.TrimSpace(lastDiffractionTitle[1:end]); num != "" {
							vizierTab.SetAsteroidNumber(num)
						}
						if name := strings.TrimSpace(lastDiffractionTitle[end+1:]); name != "" {
							vizierTab.AsteroidNameEntry.SetText(name)
						}
					}
				}
			})

			// Launch IOTAdiffraction (reuses the already-visible dialog)
			runIOTAdiffraction(paramFilePath)
		}()
	}
	afterOccParamsSaved = useParamFile

	btnOccultParams := widget.NewButton("Edit Occultation Parameters", func() {
		if loadedLightCurveData != nil && loadedLightCurveData.SourceFilePath != "" {
			obsDir := filepath.Dir(loadedLightCurveData.SourceFilePath)
			if entries, err := os.ReadDir(obsDir); err == nil {
				for _, entry := range entries {
					if !entry.IsDir() && strings.ToLower(filepath.Ext(entry.Name())) == ".occparams" {
						lastLoadedParamsPath = filepath.Join(obsDir, entry.Name())
						showOccultationParametersDialog(w, false, nil, obsDir)
						return
					}
				}
			}
			dialog.ShowInformation("No .occparams file found",
				"No .occparams file was found in the current observation folder.\n\n"+
					"Use the \"Process occelmnt file\" button to create one.", w)
			return
		}
		showOccultationParametersDialog(w, true, nil, "")
	})
	var stopProcessBlink func()
	btnProcessOccelemnt := widget.NewButton("Process occelmnt file", func() {
		// Autoload the first file whose name starts with "occ" from the CSV directory
		autoXml := ""
		if loadedLightCurveData != nil {
			if srcPath := loadedLightCurveData.SourceFilePath; srcPath != "" {
				obsDir := filepath.Dir(srcPath)
				if entries, err := os.ReadDir(obsDir); err == nil {
					for _, entry := range entries {
						if !entry.IsDir() && strings.Contains(strings.ToLower(entry.Name()), "occel") {
							fullPath := filepath.Join(obsDir, entry.Name())
							if data, rerr := os.ReadFile(fullPath); rerr == nil {
								autoXml = strings.TrimPrefix(string(data), "\xef\xbb\xbf")
								lastLoadedOccelmntXml = autoXml
								prefs.SetString("lastLoadedOccelmntXml", autoXml)
								vizierTab.FillStarFromOccelmntXml(autoXml)
								logAction(fmt.Sprintf("Auto-loaded occelmnt file: %s", fullPath))
							}
							break
						}
					}
				}
			}
		}
		showProcessOccelemntDialog(w, vizierTab, autoXml)
	})
	btnProcessOccelemnt.Importance = widget.HighImportance
	btnProcessOccelemnt.Disable()
	// startProcessBlink starts blinking the button; safe to call multiple times.
	startProcessBlink := func() {
		if stopProcessBlink != nil {
			return // already blinking
		}
		blinkStop := make(chan struct{})
		stopProcessBlink = func() { close(blinkStop) }
		go func() {
			on := true
			for {
				select {
				case <-blinkStop:
					return
				case <-time.After(600 * time.Millisecond):
					on = !on
					fyne.Do(func() {
						if on {
							btnProcessOccelemnt.Importance = widget.HighImportance
						} else {
							btnProcessOccelemnt.Importance = widget.MediumImportance
						}
						btnProcessOccelemnt.Refresh()
					})
				}
			}
		}()
	}
	ac.stopProcessOccelmntBlink = func() {
		if stopProcessBlink != nil {
			stopProcessBlink()
			stopProcessBlink = nil
		}
	}
	ac.resetProcessOccelmntBtn = func() {
		btnProcessOccelemnt.Enable()
		btnProcessOccelemnt.Importance = widget.HighImportance
		btnProcessOccelemnt.Refresh()
		startProcessBlink()
	}
	// Wrap afterOccParamsSaved to stop blinking and change the Process occelmnt button to yellow
	origAfterSaved := afterOccParamsSaved
	afterOccParamsSaved = func(path string) {
		origAfterSaved(path)
		if stopProcessBlink != nil {
			stopProcessBlink()
			stopProcessBlink = nil
		}
		btnProcessOccelemnt.Importance = widget.WarningImportance
		btnProcessOccelemnt.Refresh()
		if ac.invalidateFitCurves != nil {
			ac.invalidateFitCurves()
		}
		tabs.Select(tab10)
		if ac.autoFillSearchRange != nil {
			ac.autoFillSearchRange()
		}
	}

	btnShowDetails := widget.NewButton("Show details file", func() {
		if loadedLightCurveData == nil || loadedLightCurveData.SourceFilePath == "" {
			dialog.ShowInformation("No observation folder", "Please load a CSV file first.", w)
			return
		}
		obsDir := filepath.Dir(loadedLightCurveData.SourceFilePath)
		// Find the first file whose name contains "detail"
		detailPath := ""
		if entries, err := os.ReadDir(obsDir); err == nil {
			for _, entry := range entries {
				if !entry.IsDir() && strings.Contains(strings.ToLower(entry.Name()), "detail") {
					detailPath = filepath.Join(obsDir, entry.Name())
					break
				}
			}
		}
		if detailPath == "" {
			dialog.ShowInformation("No details file", "No file containing \"detail\" in its name was found in the current observation folder.", w)
			return
		}
		// Read and parse as CSV
		fileData, rerr := os.ReadFile(detailPath)
		if rerr != nil {
			dialog.ShowError(fmt.Errorf("failed to read details file: %w", rerr), w)
			return
		}
		// Normalize mixed line endings (CR LF and bare CR) to LF before CSV parsing.
		fileData = bytes.ReplaceAll(fileData, []byte("\r\n"), []byte("\n"))
		fileData = bytes.ReplaceAll(fileData, []byte("\r"), []byte("\n"))
		reader := csv.NewReader(bytes.NewReader(fileData))
		reader.FieldsPerRecord = -1
		reader.TrimLeadingSpace = true
		var rows [][]string
		for {
			record, err := reader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to parse details file: %w", err), w)
				return
			}
			rows = append(rows, record)
		}
		if len(rows) == 0 {
			dialog.ShowInformation("Details file", "The details file is empty.", w)
			return
		}
		numCols := 0
		for _, row := range rows {
			if len(row) > numCols {
				numCols = len(row)
			}
		}
		// Compute column widths from content
		colWidths := make([]float32, numCols)
		for _, row := range rows {
			for c, cell := range row {
				w := float32(len(cell))*7.5 + 20
				if w > colWidths[c] {
					colWidths[c] = w
				}
				if colWidths[c] < 60 {
					colWidths[c] = 60
				}
				if colWidths[c] > 300 {
					colWidths[c] = 300
				}
			}
		}
		table := widget.NewTable(
			func() (int, int) { return len(rows), numCols },
			func() fyne.CanvasObject {
				lbl := widget.NewLabel("")
				lbl.Wrapping = fyne.TextWrapOff
				return lbl
			},
			func(id widget.TableCellID, cell fyne.CanvasObject) {
				lbl := cell.(*widget.Label)
				if id.Row < len(rows) && id.Col < len(rows[id.Row]) {
					lbl.SetText(rows[id.Row][id.Col])
				} else {
					lbl.SetText("")
				}
				if id.Row == 0 {
					lbl.TextStyle = fyne.TextStyle{Bold: true}
				} else {
					lbl.TextStyle = fyne.TextStyle{}
				}
				lbl.Refresh()
			},
		)
		for col, cw := range colWidths {
			table.SetColumnWidth(col, cw)
		}
		table.OnSelected = func(id widget.TableCellID) {
			cellValue := ""
			if id.Row < len(rows) && id.Col < len(rows[id.Row]) {
				cellValue = rows[id.Row][id.Col]
			}
			w.Clipboard().SetContent(cellValue)
			dialog.ShowInformation("Copied to clipboard", fmt.Sprintf("%q", cellValue), w)
		}
		closeBtn := widget.NewButton("Close", nil)
		var dlg dialog.Dialog
		closeBtn.OnTapped = func() { dlg.Hide() }
		tableScroll := container.NewScroll(table)
		tableScroll.SetMinSize(fyne.NewSize(800, 400))
		content := container.NewBorder(nil, container.NewHBox(layout.NewSpacer(), closeBtn), nil, nil, tableScroll)
		dlg = dialog.NewCustomWithoutButtons("Details: "+filepath.Base(detailPath), content, w)
		dlg.Resize(fyne.NewSize(900, 520))
		dlg.Show()
	})
	btnShowIOTAPlots := widget.NewButton("Show IOTAdiffraction plots", func() {
		type plotInfo struct {
			path  string
			title string
		}
		plots := []plotInfo{
			{filepath.Join(appDir, "lightCurvePlot.png"), "Light Curve Plot"},
			{filepath.Join(appDir, "diffractionImageWithPath.png"), "Diffraction Image"},
			{filepath.Join(appDir, "camera_response.png"), "Camera Response"},
		}
		var images []fyne.CanvasObject
		for _, p := range plots {
			if _, err := os.Stat(p.path); err != nil {
				continue
			}
			img := canvas.NewImageFromFile(p.path)
			img.FillMode = canvas.ImageFillContain
			img.SetMinSize(fyne.NewSize(500, 400))
			label := widget.NewLabel(p.title)
			label.Alignment = fyne.TextAlignCenter
			images = append(images, container.NewBorder(nil, label, nil, nil, img))
		}
		if len(images) == 0 {
			dialog.ShowInformation("No plots found",
				"No IOTAdiffraction plot files were found in the application directory.", w)
			return
		}
		plotsWin := a.NewWindow("IOTAdiffraction Plots")
		grid := container.NewGridWithColumns(len(images))
		for _, img := range images {
			grid.Add(img)
		}
		plotsWin.SetContent(container.NewScroll(grid))
		plotsWin.Resize(fyne.NewSize(1600, 500))
		plotsWin.CenterOnScreen()
		safeShowWindow(plotsWin)
	})

	btnShowIOTAPlots.Disable()
	ac.enableShowIOTAPlots = func() { btnShowIOTAPlots.Enable() }

	var btnImageAcqTiming *widget.Button
	var stopAcqTimingBlink func()
	startAcqTimingBlink := func() {
		if stopAcqTimingBlink != nil {
			return // already blinking
		}
		blinkStop := make(chan struct{})
		stopAcqTimingBlink = func() { close(blinkStop) }
		go func() {
			on := true
			for {
				select {
				case <-blinkStop:
					return
				case <-time.After(600 * time.Millisecond):
					on = !on
					fyne.Do(func() {
						if on {
							btnImageAcqTiming.Importance = widget.HighImportance
						} else {
							btnImageAcqTiming.Importance = widget.MediumImportance
						}
						btnImageAcqTiming.Refresh()
					})
				}
			}
		}()
	}
	acqTimingConfirmed := false
	ac.startAcqTimingBlink = func() {
		if !acqTimingConfirmed {
			startAcqTimingBlink()
		}
	}
	ac.stopAcqTimingBlink = func() {
		if stopAcqTimingBlink != nil {
			stopAcqTimingBlink()
			stopAcqTimingBlink = nil
		}
		if !acqTimingConfirmed {
			btnImageAcqTiming.Importance = widget.HighImportance
			btnImageAcqTiming.Refresh()
		}
	}
	ac.confirmAcqTiming = func() {
		if stopAcqTimingBlink != nil {
			stopAcqTimingBlink()
			stopAcqTimingBlink = nil
		}
		acqTimingConfirmed = true
		btnImageAcqTiming.Importance = widget.WarningImportance
		btnImageAcqTiming.Refresh()
	}

	btnImageAcqTiming = widget.NewButton("Camera timing adjustments", func() {
		acqTimingConfirmed = false // reset so blink can start if the user cancels
		cameraNameEntry := widget.NewEntry()
		cameraNameEntry.SetPlaceHolder("camera name")
		cameraNameEntry.SetText(sessionCameraName)

		acqDelayEntry := widget.NewEntry()
		acqDelayEntry.SetPlaceHolder("msecs")
		acqDelayEntry.SetText(sessionAcqDelay)

		starRowEntry := widget.NewEntry()
		starRowEntry.SetPlaceHolder("row number")
		starRowEntry.SetText(sessionStarRow)

		rowDeltaEntry := widget.NewEntry()
		rowDeltaEntry.SetPlaceHolder("msecs")
		rowDeltaEntry.SetText(sessionRowDelta)

		// updateCameraDelay updates session variables and recomputes the camera
		// delay comment, pushing it to the open SODIS dialog (if any).
		updateCameraDelay := func() {
			sessionCameraName = cameraNameEntry.Text
			sessionAcqDelay = acqDelayEntry.Text
			sessionRowDelta = rowDeltaEntry.Text
			sessionStarRow = starRowEntry.Text

			acqDelayMs, err1 := strconv.ParseFloat(strings.TrimSpace(acqDelayEntry.Text), 64)
			starRow, err2 := strconv.ParseFloat(strings.TrimSpace(starRowEntry.Text), 64)
			rowDeltaMs, err3 := strconv.ParseFloat(strings.TrimSpace(rowDeltaEntry.Text), 64)

			acqCorrSecs := acqDelayMs / 1000.0
			hasRS := err2 == nil && err3 == nil && strings.TrimSpace(starRowEntry.Text) != "" && strings.TrimSpace(rowDeltaEntry.Text) != ""
			var rsCorrSecs float64
			if hasRS {
				rsCorrSecs = starRow * rowDeltaMs / 1000.0
			}

			var comment string
			if err1 == nil && hasRS {
				comment = fmt.Sprintf(
					"acqCorr = %.4f sec (Acquisition Delay)\nrsCorr = %.4f sec (starRow=%.1f * rowDelta=%.6f ms)",
					acqCorrSecs, rsCorrSecs, starRow, rowDeltaMs)
			} else if err1 == nil {
				comment = fmt.Sprintf("acqCorr = %.4f sec (Acquisition Delay)", acqCorrSecs)
			}
			cameraName := strings.TrimSpace(cameraNameEntry.Text)
			if cameraName != "" {
				comment += fmt.Sprintf(" [camera: %s]", cameraName)
			}
			if ac.updateSodisComment != nil {
				ac.updateSodisComment(comment)
			}
		}
		acqDelayEntry.OnChanged = func(_ string) { updateCameraDelay() }
		starRowEntry.OnChanged = func(_ string) { updateCameraDelay() }
		rowDeltaEntry.OnChanged = func(_ string) { updateCameraDelay() }
		cameraNameEntry.OnChanged = func(_ string) { updateCameraDelay() }

		formItems := []*widget.FormItem{
			{Text: "Camera name (optional)", Widget: cameraNameEntry},
			{Text: "Acquisition delay (msecs)", Widget: acqDelayEntry},
			{Text: "star row position at occultation", Widget: starRowEntry},
			{Text: "row-to-row time delta (msecs)", Widget: rowDeltaEntry},
		}
		dlg := dialog.NewForm("Camera Timing Adjustments", "OK", "Cancel", formItems, func(ok bool) {
			if !ok {
				// Canceled: start blinking as a reminder if on the Fit tab
				if onFitTab {
					startAcqTimingBlink()
				}
				return
			}
			sessionCameraName = cameraNameEntry.Text
			sessionAcqDelay = acqDelayEntry.Text
			sessionRowDelta = rowDeltaEntry.Text
			sessionStarRow = starRowEntry.Text
			// Write camera-timing.txt to the observation directory
			if resultsFolder != "" {
				obsDir := filepath.Dir(resultsFolder)
				content := fmt.Sprintf("cameraName=%s\nacqDelay=%s\nstarRow=%s\nrowDelta=%s\n",
					sessionCameraName, sessionAcqDelay, sessionStarRow, sessionRowDelta)
				if err := os.WriteFile(filepath.Join(obsDir, "camera-timing.txt"), []byte(content), 0644); err != nil {
					fmt.Printf("Warning: could not write camera-timing.txt: %v\n", err)
				}
			}
			ac.confirmAcqTiming()
		}, w)
		dlg.Resize(fyne.NewSize(450, 250))
		dlg.Show()
	})
	btnImageAcqTiming.Importance = widget.HighImportance

	btnCheckForUpdates := widget.NewButton("Check for updates", func() {
		ShowUpdateDialogTwoPane(w)
	})

	buttons := container.NewHBox(btnCheckForUpdates, btnProcessOccelemnt, btnImageAcqTiming, btnOccultParams, btnShowDetails, btnShowIOTAPlots)

	// Split tabs and plot area
	split := container.NewHSplit(tabs, plotArea)
	splitOffset := prefs.FloatWithFallback("splitOffset", 0.35)
	split.SetOffset(splitOffset)

	content := container.NewBorder(nil, buttons, nil, nil, split)

	w.SetContent(content)
	w.Resize(fyne.NewSize(float32(savedW), float32(savedH)))

	if firstRun {
		w.CenterOnScreen()
	}

	// Save window geometry and split position on close
	w.SetCloseIntercept(func() {
		doClose := func() {
			prefs.SetFloat("splitOffset", split.Offset)
			if hwnd := getForegroundWindow(); hwnd != 0 {
				if x, y, width, height, ok := getWindowRect(hwnd); ok {
					prefs.SetInt("windowX", int(x))
					prefs.SetInt("windowY", int(y))
					prefs.SetInt("windowW", int(width))
					prefs.SetInt("windowH", int(height))
				}
			}
			closeActionLog() // Close the action log file
			w.Close()
		}

		if sodisReportSavedThisSession && !vizierDatWrittenThisSession && !sodisNegativeReportSaved {
			dialog.ShowConfirm("VizieR file not written",
				"A SODIS report was saved but a VizieR .dat file has not been written.\n\nDo you really want to exit?",
				func(confirmed bool) {
					if confirmed {
						doClose()
					}
				}, w)
			return
		}
		doClose()
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
