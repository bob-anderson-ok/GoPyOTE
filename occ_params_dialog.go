package main

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
	"github.com/KevinWang15/go-json5"
)

func showOccultationParametersDialog(w fyne.Window, clearAll bool, preload *OccultationParameters, obsDir string) {
	// Build dropdown choices from files in the CAMERA-QE folder
	cameraQEDir := filepath.Join(appDir, "CAMERA-QE")
	qeFileNames := []string{"(none)"}
	if dirEntries, err := os.ReadDir(cameraQEDir); err == nil {
		for _, entry := range dirEntries {
			if !entry.IsDir() {
				qeFileNames = append(qeFileNames, entry.Name())
			}
		}
	}

	// Create entry fields for all parameters
	windowSizeEntry := widget.NewEntry()
	titleEntry := widget.NewEntry()
	fundamentalPlaneWidthKmEntry := widget.NewEntry()
	fundamentalPlaneWidthNumPointsEntry := widget.NewEntry()
	parallaxArcsecEntry := widget.NewEntry()
	distanceAuEntry := widget.NewEntry()
	pathToQeTableFileSelect := widget.NewSelect(qeFileNames, nil)
	// setQeSelected sets the dropdown from a stored path (may include CAMERA-QE/ prefix)
	setQeSelected := func(path string) {
		if path == "" {
			pathToQeTableFileSelect.SetSelected("(none)")
			return
		}
		pathToQeTableFileSelect.SetSelected(filepath.Base(path))
	}
	// getQeSelected returns the selected QE file name, or "" when "(none)" is chosen.
	getQeSelected := func() string {
		if pathToQeTableFileSelect.Selected == "(none)" {
			return ""
		}
		return pathToQeTableFileSelect.Selected
	}
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

	// Label to display the loaded parameters file name
	fileNameLabel := widget.NewLabel("")
	fileNameLabel.TextStyle = fyne.TextStyle{Bold: true}

	// Restore last loaded parameters path from preferences
	prefs := fyne.CurrentApp().Preferences()
	if lastLoadedParamsPath == "" {
		lastLoadedParamsPath = prefs.StringWithFallback("lastLoadedParamsPath", "")
	}

	// occelmntXml associated with the currently displayed params file (empty if none)
	var dialogOccelmntXml string

	// populateEntries fills all entry fields from an OccultationParameters struct.
	populateEntries := func(params *OccultationParameters) {
		dialogOccelmntXml = params.OccelmntXml
		windowSizeEntry.SetText(strconv.Itoa(params.WindowSizePixels))
		titleEntry.SetText(params.Title)
		fundamentalPlaneWidthKmEntry.SetText(strconv.FormatFloat(params.FundamentalPlaneWidthKm, 'f', -1, 64))
		fundamentalPlaneWidthNumPointsEntry.SetText(strconv.Itoa(params.FundamentalPlaneWidthNumPoints))
		parallaxArcsecEntry.SetText(strconv.FormatFloat(params.ParallaxArcsec, 'f', -1, 64))
		distanceAuEntry.SetText(strconv.FormatFloat(params.DistanceAu, 'f', -1, 64))
		setQeSelected(params.PathToQeTableFile)
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
	}

	// Autoload previously opened parameters file if available (skipped when clearAll or preload is set)
	if !clearAll && preload == nil && lastLoadedParamsPath != "" {
		file, err := os.Open(lastLoadedParamsPath)
		if err == nil {
			params, parseErr := parseOccultationParameters(file)
			if closeErr := file.Close(); closeErr != nil {
				dialog.ShowError(fmt.Errorf("failed to close file: %w", closeErr), w)
			}
			if parseErr == nil {
				populateEntries(params)
				loadedFileName = filepath.Base(lastLoadedParamsPath)
				fileNameLabel.SetText("File being displayed:  " + loadedFileName)
				logAction(fmt.Sprintf("Auto-loaded parameters file: %s", lastLoadedParamsPath))
			}
		}
	}

	// Populate entries from preload if provided (e.g., from Create Occultation)
	if preload != nil {
		populateEntries(preload)
		fileNameLabel.SetText("New parameters — use Write to save")
	}

	// If the QE file dropdown has no real file selected, fill from the saved preference
	if pathToQeTableFileSelect.Selected == "" || pathToQeTableFileSelect.Selected == "(none)" {
		if savedQe := prefs.StringWithFallback("stickyQeTableFile", ""); savedQe != "" {
			setQeSelected(savedQe)
		}
	}

	// Collect all entries for dirty-checking on Cancel
	allEntries := []*widget.Entry{
		windowSizeEntry, titleEntry, fundamentalPlaneWidthKmEntry,
		fundamentalPlaneWidthNumPointsEntry, parallaxArcsecEntry, distanceAuEntry,
		observationWavelengthNmEntry, dXKmPerSecEntry,
		dYKmPerSecEntry, pathPerpendicularOffsetKmEntry, percentMagDropEntry,
		starDiamOnPlaneMasEntry, limbDarkeningCoeffEntry, starClassEntry,
		mainBodyXCenterEntry, mainBodyYCenterEntry, mainBodyMajorAxisEntry,
		mainBodyMinorAxisEntry, mainBodyPaDegreesEntry,
		satelliteXCenterEntry, satelliteYCenterEntry, satelliteMajorAxisEntry,
		satelliteMinorAxisEntry, satellitePaDegreesEntry,
		pathToExternalImageEntry,
	}
	// Snapshot initial values so we can detect edits
	initialValues := make([]string, len(allEntries))
	for i, e := range allEntries {
		initialValues[i] = e.Text
	}
	qeInitialValue := pathToQeTableFileSelect.Selected

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
		&widget.FormItem{Text: "Name of camera QE file", Widget: pathToQeTableFileSelect},
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

	// Create a custom dialog with Browse/Write/Cancel buttons
	var customDialog *dialog.CustomDialog
	cancelBtn := widget.NewButton("Cancel", func() {
		// Check if any entry has been modified
		dirty := pathToQeTableFileSelect.Selected != qeInitialValue
		for i, e := range allEntries {
			if e.Text != initialValues[i] {
				dirty = true
				break
			}
		}
		if dirty {
			dialog.ShowConfirm("Unsaved Changes",
				"You have unsaved changes. Use Write to save them.\n\nDiscard changes and close?",
				func(confirmed bool) {
					if confirmed {
						customDialog.Hide()
					}
				}, w)
		} else {
			customDialog.Hide()
		}
	})

	// File open button
	loadBtn := widget.NewButton("Browse", func() {
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

			populateEntries(params)

			// Store the loaded file name for use as default in the save dialog
			loadedFileName = reader.URI().Name()
			// Store the full path for use by IOTAdiffraction
			lastLoadedParamsPath = reader.URI().Path()
			// Persist to preferences so it autoloads next time
			prefs.SetString("lastLoadedParamsPath", lastLoadedParamsPath)
			fileNameLabel.SetText("File being displayed:  " + loadedFileName)
			logAction(fmt.Sprintf("Loaded parameters file: %s", lastLoadedParamsPath))
			// Re-snapshot so a fresh load is considered clean
			for i, e := range allEntries {
				initialValues[i] = e.Text
			}
			qeInitialValue = pathToQeTableFileSelect.Selected
		}, w)
		fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".occparams"}))
		occParamsDir := filepath.Join(appDir, "OCCULTATION-PARAMETERS")
		folderURI := storage.NewFileURI(occParamsDir)
		if listableURI, err := storage.ListerForURI(folderURI); err == nil {
			fileDialog.SetLocation(listableURI)
		}
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
	saveBtn := widget.NewButton("Write", func() {
		doSave := func() {
			if obsDir != "" {
				// Auto-save directly to the observation folder — no file dialog
				saveFileName := "occultation.occparams"
				if title := strings.TrimSpace(titleEntry.Text); title != "" {
					sanitized := strings.Map(func(r rune) rune {
						if strings.ContainsRune(`\/:*?"<>|`, r) {
							return '_'
						}
						return r
					}, title)
					saveFileName = sanitized + ".occparams"
				} else if loadedFileName != "" {
					saveFileName = loadedFileName
				}
				savePath := filepath.Join(obsDir, saveFileName)
				// Build parameters struct from entry fields
				params := OccultationParameters{
					WindowSizePixels:               parseInt(windowSizeEntry.Text),
					Title:                          titleEntry.Text,
					FundamentalPlaneWidthKm:        parseFloat(fundamentalPlaneWidthKmEntry.Text),
					FundamentalPlaneWidthNumPoints: parseInt(fundamentalPlaneWidthNumPointsEntry.Text),
					ParallaxArcsec:                 parseFloat(parallaxArcsecEntry.Text),
					DistanceAu:                     parseFloat(distanceAuEntry.Text),
					PathToQeTableFile:              getQeSelected(),
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
					ExposureTimeSecs:    0,
					OccelmntXml:         dialogOccelmntXml,
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

				// Write to the file with the enforced extension
				if werr := os.WriteFile(savePath, data, 0644); werr != nil {
					dialog.ShowError(fmt.Errorf("failed to write file: %w", werr), w)
					return
				}

				logOccparamsWrite("dialog save", savePath)

				// Track the saved path so CSV-read autofill and future Browse defaults use it
				lastLoadedParamsPath = savePath
				prefs.SetString("lastLoadedParamsPath", lastLoadedParamsPath)
				loadedFileName = filepath.Base(savePath)
				fileNameLabel.SetText("File being displayed:  " + loadedFileName)

				// Persist QE file name so it autofills next time
				if qe := getQeSelected(); qe != "" {
					prefs.SetString("stickyQeTableFile", qe)
				}

				// Re-snapshot so saved state is considered clean
				for i, e := range allEntries {
					initialValues[i] = e.Text
				}
				qeInitialValue = pathToQeTableFileSelect.Selected

				// Close the parameters dialog and defer the callback so the
				// dialog visually disappears before IOTAdiffraction starts.
				customDialog.Hide()
				if afterOccParamsSaved != nil {
					cb := afterOccParamsSaved
					sp := savePath
					time.AfterFunc(50*time.Millisecond, func() {
						fyne.Do(func() { cb(sp) })
					})
				}
				return
			}

			fileDialog := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
				if err != nil {
					dialog.ShowError(err, w)
					return
				}
				if writer == nil {
					return // User cancelled
				}

				// Enforce .occparams extension
				originalSavePath := writer.URI().Path()
				savePath := originalSavePath
				ext := filepath.Ext(savePath)
				if strings.ToLower(ext) != ".occparams" {
					savePath = strings.TrimSuffix(savePath, ext) + ".occparams"
				}

				// Close the writer first so the file handle is released
				if cerr := writer.Close(); cerr != nil {
					dialog.ShowError(fmt.Errorf("failed to close file: %w", cerr), w)
				}

				// Remove the empty file created by the save dialog if the path changed
				if savePath != originalSavePath {
					if rerr := os.Remove(originalSavePath); rerr != nil {
						fmt.Printf("Warning: could not remove empty file %s: %v\n", originalSavePath, rerr)
					}
				}

				// Build parameters struct from entry fields
				params := OccultationParameters{
					WindowSizePixels:               parseInt(windowSizeEntry.Text),
					Title:                          titleEntry.Text,
					FundamentalPlaneWidthKm:        parseFloat(fundamentalPlaneWidthKmEntry.Text),
					FundamentalPlaneWidthNumPoints: parseInt(fundamentalPlaneWidthNumPointsEntry.Text),
					ParallaxArcsec:                 parseFloat(parallaxArcsecEntry.Text),
					DistanceAu:                     parseFloat(distanceAuEntry.Text),
					PathToQeTableFile:              getQeSelected(),
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
					ExposureTimeSecs:    0,
					OccelmntXml:         dialogOccelmntXml,
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

				// Write to the file with the enforced extension
				if werr := os.WriteFile(savePath, data, 0644); werr != nil {
					dialog.ShowError(fmt.Errorf("failed to write file: %w", werr), w)
					return
				}

				logOccparamsWrite("dialog save", savePath)

				// Track the saved path so CSV-read autofill and future Browse defaults use it
				lastLoadedParamsPath = savePath
				prefs.SetString("lastLoadedParamsPath", lastLoadedParamsPath)
				loadedFileName = filepath.Base(savePath)
				fileNameLabel.SetText("File being displayed:  " + loadedFileName)

				// Persist QE file name so it autofills next time
				if qe := getQeSelected(); qe != "" {
					prefs.SetString("stickyQeTableFile", qe)
				}

				// Re-snapshot so saved state is considered clean
				for i, e := range allEntries {
					initialValues[i] = e.Text
				}
				qeInitialValue = pathToQeTableFileSelect.Selected

				// Close the parameters dialog and defer the callback so the
				// dialog visually disappears before IOTAdiffraction starts.
				customDialog.Hide()
				if afterOccParamsSaved != nil {
					cb := afterOccParamsSaved
					sp := savePath
					time.AfterFunc(50*time.Millisecond, func() {
						fyne.Do(func() { cb(sp) })
					})
				}
			}, w)
			fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".occparams"}))
			if loadedFileName != "" {
				fileDialog.SetFileName(loadedFileName)
			} else if title := strings.TrimSpace(titleEntry.Text); title != "" {
				sanitized := strings.Map(func(r rune) rune {
					if strings.ContainsRune(`\/:*?"<>|`, r) {
						return '_'
					}
					return r
				}, title)
				fileDialog.SetFileName(sanitized + ".occparams")
			}
			// Always save to the OCCULTATION-PARAMETERS directory, creating it if needed
			occParamsDir := filepath.Join(appDir, "OCCULTATION-PARAMETERS")
			if merr := os.MkdirAll(occParamsDir, 0755); merr != nil {
				dialog.ShowError(fmt.Errorf("failed to create OCCULTATION-PARAMETERS directory: %w", merr), w)
				return
			}
			folderURI := storage.NewFileURI(occParamsDir)
			if listableURI, locErr := storage.ListerForURI(folderURI); locErr == nil {
				fileDialog.SetLocation(listableURI)
			}
			fileDialog.Resize(fyne.NewSize(1200, 800))
			fileDialog.Show()
		} // end doSave

		if pathToQeTableFileSelect.Selected == "" {
			dialog.ShowConfirm(
				"No camera QE file",
				"No camera QE file is selected. Save without one?",
				func(saveWithout bool) {
					if saveWithout {
						doSave()
					}
				}, w)
			return
		}
		doSave()
	})
	saveBtn.Importance = widget.HighImportance

	showOccelmntBtn := widget.NewButton("Show associated occelmnt.xml", func() {
		if dialogOccelmntXml == "" {
			dialog.ShowInformation("No occelmnt.xml data", "No occelmnt.xml data is associated with the current parameters file.", w)
			return
		}
		xmlEntry := widget.NewMultiLineEntry()
		xmlEntry.SetText(dialogOccelmntXml)
		xmlEntry.Wrapping = fyne.TextWrapOff
		scroll := container.NewVScroll(xmlEntry)
		scroll.SetMinSize(fyne.NewSize(800, 300))
		d := dialog.NewCustom("Associated occelmnt.xml", "Close", scroll, w)
		d.Resize(fyne.NewSize(840, 400))
		d.Show()
	})

	buttons := container.NewHBox(loadBtn, saveBtn, showOccelmntBtn, layout.NewSpacer(), cancelBtn)
	bottomSection := container.NewVBox(fileNameLabel, buttons)
	content := container.NewBorder(nil, bottomSection, nil, nil, scrollContent)

	customDialog = dialog.NewCustomWithoutButtons("Edit Occultation Parameters", content, w)
	customDialog.Resize(fyne.NewSize(840, 750))
	customDialog.Show()
}

func showProcessOccelemntDialog(w fyne.Window, vt *VizieRTab, initialXml string) {
	pasteEntry := widget.NewMultiLineEntry()
	pasteEntry.SetPlaceHolder("Use the load button above or, in OWC, click copy to clipboard, then paste (Ctrl V) to fill this panel.")
	pasteEntry.Wrapping = fyne.TextWrapOff
	if initialXml != "" {
		pasteEntry.SetText(initialXml)
	}

	// --- Load occelmnt file button ---
	loadOccelmntBtn := widget.NewButton("Load Occelmnt file", func() {
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
			data, rerr := io.ReadAll(reader)
			if rerr != nil {
				dialog.ShowError(fmt.Errorf("failed to read occelmnt file: %w", rerr), w)
				return
			}
			xmlStr := strings.TrimPrefix(string(data), "\xef\xbb\xbf")
			pasteEntry.SetText(xmlStr)
			lastLoadedOccelmntXml = xmlStr
			fyne.CurrentApp().Preferences().SetString("lastLoadedOccelmntXml", xmlStr)
			vt.FillStarFromOccelmntXml(xmlStr)
			logAction(fmt.Sprintf("Loaded occelmnt file: %s", reader.URI().Path()))
		}, w)
		fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".xml", ".txt"}))
		if loadedLightCurveData != nil && loadedLightCurveData.SourceFilePath != "" {
			obsDir := filepath.Dir(loadedLightCurveData.SourceFilePath)
			folderURI := storage.NewFileURI(obsDir)
			if listableURI, lerr := storage.ListerForURI(folderURI); lerr == nil {
				fileDialog.SetLocation(listableURI)
			}
		}
		fileDialog.Resize(fyne.NewSize(800, 600))
		fileDialog.Show()
	})

	pasteEntry.OnChanged = func(text string) {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return
		}
		if loadedLightCurveData == nil || loadedLightCurveData.SourceFilePath == "" {
			return
		}
		obsDir := filepath.Dir(loadedLightCurveData.SourceFilePath)
		savePath := filepath.Join(obsDir, "occelmnt-pasted.xml")
		if err := os.WriteFile(savePath, []byte(trimmed), 0644); err != nil {
			fmt.Printf("Warning: could not write %s: %v\n", savePath, err)
		} else {
			logAction(fmt.Sprintf("Pasted occelmnt saved to %s", savePath))
		}
	}

	scrollable := container.NewVScroll(pasteEntry)
	scrollable.SetMinSize(fyne.NewSize(800, 300))

	// --- Site Location section ---
	longDegEntry := widget.NewEntry()
	longDegEntry.SetPlaceHolder("+/-deg")
	longMinEntry := widget.NewEntry()
	longMinEntry.SetPlaceHolder("min")
	longSecsEntry := widget.NewEntry()
	longSecsEntry.SetPlaceHolder("sec")
	longDegContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(70, 36)), longDegEntry)
	longMinContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(60, 36)), longMinEntry)
	longSecsContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(70, 36)), longSecsEntry)

	longDecimalEntry := widget.NewEntry()
	longDecimalEntry.SetPlaceHolder("decimal")
	longDecimalContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(120, 36)), longDecimalEntry)

	longUpdating := false

	updateLongDecimal := func() {
		if longUpdating {
			return
		}
		longUpdating = true
		defer func() { longUpdating = false }()
		degVal, err1 := strconv.ParseFloat(strings.TrimSpace(longDegEntry.Text), 64)
		minVal, err2 := strconv.ParseFloat(strings.TrimSpace(longMinEntry.Text), 64)
		secVal, err3 := strconv.ParseFloat(strings.TrimSpace(longSecsEntry.Text), 64)
		if err1 != nil || err2 != nil || err3 != nil {
			longDecimalEntry.SetText("")
			return
		}
		sign := 1.0
		if degVal < 0 {
			sign = -1.0
			degVal = -degVal
		}
		decimal := sign * (degVal + minVal/60.0 + secVal/3600.0)
		longDecimalEntry.SetText(fmt.Sprintf("%.6f", decimal))
	}

	updateLongDMS := func() {
		if longUpdating {
			return
		}
		longUpdating = true
		defer func() { longUpdating = false }()
		decVal, err := strconv.ParseFloat(strings.TrimSpace(longDecimalEntry.Text), 64)
		if err != nil {
			longDegEntry.SetText("")
			longMinEntry.SetText("")
			longSecsEntry.SetText("")
			return
		}
		deg, minute, sec := decimalToDMS(decVal)
		longDegEntry.SetText(deg)
		longMinEntry.SetText(minute)
		longSecsEntry.SetText(sec)
	}

	longDegEntry.OnChanged = func(_ string) { updateLongDecimal() }
	longMinEntry.OnChanged = func(_ string) { updateLongDecimal() }
	longSecsEntry.OnChanged = func(_ string) { updateLongDecimal() }
	longDecimalEntry.OnChanged = func(_ string) { updateLongDMS() }

	latDegEntry := widget.NewEntry()
	latDegEntry.SetPlaceHolder("+/-deg")
	latMinEntry := widget.NewEntry()
	latMinEntry.SetPlaceHolder("min")
	latSecsEntry := widget.NewEntry()
	latSecsEntry.SetPlaceHolder("sec")
	latDegContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(70, 36)), latDegEntry)
	latMinContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(60, 36)), latMinEntry)
	latSecsContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(70, 36)), latSecsEntry)

	latDecimalEntry := widget.NewEntry()
	latDecimalEntry.SetPlaceHolder("decimal")
	latDecimalContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(120, 36)), latDecimalEntry)

	latUpdating := false

	updateLatDecimal := func() {
		if latUpdating {
			return
		}
		latUpdating = true
		defer func() { latUpdating = false }()
		degVal, err1 := strconv.ParseFloat(strings.TrimSpace(latDegEntry.Text), 64)
		minVal, err2 := strconv.ParseFloat(strings.TrimSpace(latMinEntry.Text), 64)
		secVal, err3 := strconv.ParseFloat(strings.TrimSpace(latSecsEntry.Text), 64)
		if err1 != nil || err2 != nil || err3 != nil {
			latDecimalEntry.SetText("")
			return
		}
		sign := 1.0
		if degVal < 0 {
			sign = -1.0
			degVal = -degVal
		}
		decimal := sign * (degVal + minVal/60.0 + secVal/3600.0)
		latDecimalEntry.SetText(fmt.Sprintf("%.6f", decimal))
	}

	updateLatDMS := func() {
		if latUpdating {
			return
		}
		latUpdating = true
		defer func() { latUpdating = false }()
		decVal, err := strconv.ParseFloat(strings.TrimSpace(latDecimalEntry.Text), 64)
		if err != nil {
			latDegEntry.SetText("")
			latMinEntry.SetText("")
			latSecsEntry.SetText("")
			return
		}
		deg, minute, sec := decimalToDMS(decVal)
		latDegEntry.SetText(deg)
		latMinEntry.SetText(minute)
		latSecsEntry.SetText(sec)
	}

	latDegEntry.OnChanged = func(_ string) { updateLatDecimal() }
	latMinEntry.OnChanged = func(_ string) { updateLatDecimal() }
	latSecsEntry.OnChanged = func(_ string) { updateLatDecimal() }
	latDecimalEntry.OnChanged = func(_ string) { updateLatDMS() }

	altitudeEntry := widget.NewEntry()
	altitudeEntry.SetPlaceHolder("meters")
	altitudeContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(80, 36)), altitudeEntry)

	// --- Observer / Equipment entries ---
	observer1Entry := widget.NewEntry()
	observer1Entry.SetPlaceHolder("first observer name")
	observer1Container := container.New(layout.NewGridWrapLayout(fyne.NewSize(200, 36)), observer1Entry)

	observer2Entry := widget.NewEntry()
	observer2Entry.SetPlaceHolder("second observer (optional)")
	observer2Container := container.New(layout.NewGridWrapLayout(fyne.NewSize(200, 36)), observer2Entry)

	observatoryEntry := widget.NewEntry()
	observatoryEntry.SetPlaceHolder("observatory name")
	observatoryContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(200, 36)), observatoryEntry)

	emailEntry := widget.NewEntry()
	emailEntry.SetPlaceHolder("e-mail address")
	emailContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(200, 36)), emailEntry)

	addressEntry := widget.NewEntry()
	addressEntry.SetPlaceHolder("postal address")
	addressContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(280, 36)), addressEntry)

	nearestCityEntry := widget.NewEntry()
	nearestCityEntry.SetPlaceHolder("nearest city")
	nearestCityContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(160, 36)), nearestCityEntry)

	countryCodeEntry := widget.NewEntry()
	countryCodeEntry.SetPlaceHolder("e.g. US")
	countryCodeContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(60, 36)), countryCodeEntry)

	// Telescope dropdown
	telescopeOpts := []string{"unstated", "1=Refractor", "2=Newtonian", "3=SCT", "4=Dobsonian", "5=Binoculars", "6=Other", "7=None", "8=eVscope"}
	telescopeOptToVal := map[string]string{
		"unstated": "", "1=Refractor": "1", "2=Newtonian": "2", "3=SCT": "3",
		"4=Dobsonian": "4", "5=Binoculars": "5", "6=Other": "6", "7=None": "7", "8=eVscope": "8",
	}
	telescopeValToOpt := map[string]string{
		"": "unstated", "1": "1=Refractor", "2": "2=Newtonian", "3": "3=SCT",
		"4": "4=Dobsonian", "5": "5=Binoculars", "6": "6=Other", "7": "7=None", "8": "8=eVscope",
	}
	telescopeSelect := widget.NewSelect(telescopeOpts, nil)
	telescopeSelect.SetSelected("unstated")
	telescopeContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(220, 36)), telescopeSelect)

	apertureEntry := widget.NewEntry()
	apertureEntry.SetPlaceHolder("e.g. 12cm")
	apertureContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(90, 36)), apertureEntry)

	focalLengthEntry := widget.NewEntry()
	focalLengthEntry.SetPlaceHolder("e.g. 120cm")
	focalLengthContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(90, 36)), focalLengthEntry)

	// Observing method dropdown
	observingMethodOpts := []string{"unspecified", "a=Analogue & digital video", "b=Digital SLR-camera video", "c=Photometer", "d=Sequential images", "e=Drift scan", "f=Visual", "g=Other"}
	observingMethodOptToVal := map[string]string{
		"unspecified": "", "a=Analogue & digital video": "a", "b=Digital SLR-camera video": "b",
		"c=Photometer": "c", "d=Sequential images": "d", "e=Drift scan": "e", "f=Visual": "f", "g=Other": "g",
	}
	observingMethodValToOpt := map[string]string{
		"": "unspecified", "a": "a=Analogue & digital video", "b": "b=Digital SLR-camera video",
		"c": "c=Photometer", "d": "d=Sequential images", "e": "e=Drift scan", "f": "f=Visual", "g": "g=Other",
	}
	observingMethodSelect := widget.NewSelect(observingMethodOpts, nil)
	observingMethodSelect.SetSelected("unspecified")
	observingMethodContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(250, 36)), observingMethodSelect)

	// Time source dropdown
	timeSourceOpts := []string{"unspecified", "a=GPS", "b=NTP", "c=Telephone (fixed or mobile)", "d=Radio time signal", "e=Internal clock of recorder", "f=Stopwatch", "g=Other"}
	timeSourceOptToVal := map[string]string{
		"unspecified": "", "a=GPS": "a", "b=NTP": "b", "c=Telephone (fixed or mobile)": "c",
		"d=Radio time signal": "d", "e=Internal clock of recorder": "e", "f=Stopwatch": "f", "g=Other": "g",
	}
	timeSourceValToOpt := map[string]string{
		"": "unspecified", "a": "a=GPS", "b": "b=NTP", "c": "c=Telephone (fixed or mobile)",
		"d": "d=Radio time signal", "e": "e=Internal clock of recorder", "f": "f=Stopwatch", "g": "g=Other",
	}
	timeSourceSelect := widget.NewSelect(timeSourceOpts, nil)
	timeSourceSelect.SetSelected("unspecified")
	timeSourceContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(250, 36)), timeSourceSelect)

	cameraEntry := widget.NewEntry()
	cameraEntry.SetPlaceHolder("camera model")
	cameraContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(200, 36)), cameraEntry)

	observerEquipSection := container.NewVBox(
		widget.NewLabel("Observer / Equipment:"),
		container.NewHBox(widget.NewLabel("Observer1:"), observer1Container, layout.NewSpacer(), widget.NewLabel("Observer2:"), observer2Container),
		container.NewHBox(widget.NewLabel("Observatory:"), observatoryContainer, layout.NewSpacer(), widget.NewLabel("E-mail:"), emailContainer),
		container.NewHBox(widget.NewLabel("Address:"), addressContainer, layout.NewSpacer(), widget.NewLabel("NearestCity:"), nearestCityContainer, layout.NewSpacer(), widget.NewLabel("CountryCode:"), countryCodeContainer),
		container.NewHBox(widget.NewLabel("Telescope:"), telescopeContainer, layout.NewSpacer(), widget.NewLabel("Aperture:"), apertureContainer, layout.NewSpacer(), widget.NewLabel("FocalLength:"), focalLengthContainer),
		container.NewHBox(widget.NewLabel("ObservingMethod:"), observingMethodContainer, layout.NewSpacer(), widget.NewLabel("TimeSource:"), timeSourceContainer, layout.NewSpacer(), widget.NewLabel("Camera:"), cameraContainer),
	)

	siteLocationSection := container.NewVBox(
		widget.NewLabel("Site Location:"),
		container.NewHBox(widget.NewLabel("Longitude(DMS):"), longDegContainer, widget.NewLabel("\u00b0"), longMinContainer, widget.NewLabel("'"), longSecsContainer, widget.NewLabel("\""), layout.NewSpacer(), widget.NewLabel("Longitude(degrees):"), longDecimalContainer),
		container.NewHBox(widget.NewLabel("Latitude(DMS):"), latDegContainer, widget.NewLabel("\u00b0"), latMinContainer, widget.NewLabel("'"), latSecsContainer, widget.NewLabel("\""), layout.NewSpacer(), widget.NewLabel("Latitude(degrees):"), latDecimalContainer),
		container.NewHBox(widget.NewLabel("Altitude (m):"), altitudeContainer),
	)

	// Flag to suppress enabling writeSiteBtn during programmatic SetText calls
	suppressWriteSiteEnable := false
	var writeSiteBtn *widget.Button

	// --- Load site file button ---
	loadSiteBtn := widget.NewButton("Load site file", func() {
		fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
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

			// Clear all entry fields before loading (suppress Write button enable)
			suppressWriteSiteEnable = true
			latDecimalEntry.SetText("")
			longDecimalEntry.SetText("")
			altitudeEntry.SetText("")
			observer1Entry.SetText("")
			observer2Entry.SetText("")
			observatoryEntry.SetText("")
			emailEntry.SetText("")
			addressEntry.SetText("")
			nearestCityEntry.SetText("")
			countryCodeEntry.SetText("")
			telescopeSelect.SetSelected("unstated")
			apertureEntry.SetText("")
			focalLengthEntry.SetText("")
			observingMethodSelect.SetSelected("unspecified")
			timeSourceSelect.SetSelected("unspecified")
			cameraEntry.SetText("")

			// Parse the site file and fill the entry fields
			file, ferr := os.Open(filePath)
			if ferr != nil {
				suppressWriteSiteEnable = false
				dialog.ShowError(fmt.Errorf("error opening site file: %w", ferr), w)
				return
			}
			defer func() {
				if cerr := file.Close(); cerr != nil {
					dialog.ShowError(fmt.Errorf("error closing site file: %w", cerr), w)
				}
			}()

			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				line := scanner.Text()

				if strings.HasPrefix(line, "latitude_decimal:") {
					value := strings.TrimSpace(strings.TrimPrefix(line, "latitude_decimal:"))
					latDecimalEntry.SetText(value)
					if value != "" {
						if lat, err := strconv.ParseFloat(value, 64); err == nil {
							deg, minutes, sec := decimalToDMS(lat)
							vt.SiteLatDegEntry.SetText(deg)
							vt.SiteLatMinEntry.SetText(minutes)
							vt.SiteLatSecsEntry.SetText(sec)
						}
					}
					continue
				}

				if strings.HasPrefix(line, "longitude_decimal:") {
					value := strings.TrimSpace(strings.TrimPrefix(line, "longitude_decimal:"))
					longDecimalEntry.SetText(value)
					if value != "" {
						if lon, err := strconv.ParseFloat(value, 64); err == nil {
							deg, minutes, sec := decimalToDMS(lon)
							vt.SiteLongDegEntry.SetText(deg)
							vt.SiteLongMinEntry.SetText(minutes)
							vt.SiteLongSecsEntry.SetText(sec)
						}
					}
					continue
				}

				if strings.HasPrefix(line, "altitude:") {
					value := strings.TrimSpace(strings.TrimPrefix(line, "altitude:"))
					altitudeEntry.SetText(value)
					if value != "" {
						vt.SiteAltitudeEntry.SetText(value)
					}
					continue
				}
				if strings.HasPrefix(line, "observer1:") {
					val := strings.TrimSpace(strings.TrimPrefix(line, "observer1:"))
					observer1Entry.SetText(val)
					vt.ObserverNameEntry.SetText(val)
					continue
				}
				if strings.HasPrefix(line, "observer2:") {
					observer2Entry.SetText(strings.TrimSpace(strings.TrimPrefix(line, "observer2:")))
					continue
				}
				if strings.HasPrefix(line, "observatory:") {
					observatoryEntry.SetText(strings.TrimSpace(strings.TrimPrefix(line, "observatory:")))
					continue
				}
				if strings.HasPrefix(line, "email:") {
					emailEntry.SetText(strings.TrimSpace(strings.TrimPrefix(line, "email:")))
					continue
				}
				if strings.HasPrefix(line, "address:") {
					addressEntry.SetText(strings.TrimSpace(strings.TrimPrefix(line, "address:")))
					continue
				}
				if strings.HasPrefix(line, "nearest_city:") {
					nearestCityEntry.SetText(strings.TrimSpace(strings.TrimPrefix(line, "nearest_city:")))
					continue
				}
				if strings.HasPrefix(line, "country_code:") {
					countryCodeEntry.SetText(strings.TrimSpace(strings.TrimPrefix(line, "country_code:")))
					continue
				}
				if strings.HasPrefix(line, "telescope:") {
					val := strings.TrimSpace(strings.TrimPrefix(line, "telescope:"))
					if opt, ok := telescopeValToOpt[val]; ok {
						telescopeSelect.SetSelected(opt)
					}
					continue
				}
				if strings.HasPrefix(line, "aperture:") {
					apertureEntry.SetText(strings.TrimSpace(strings.TrimPrefix(line, "aperture:")))
					continue
				}
				if strings.HasPrefix(line, "focal_length:") {
					focalLengthEntry.SetText(strings.TrimSpace(strings.TrimPrefix(line, "focal_length:")))
					continue
				}
				if strings.HasPrefix(line, "observing_method:") {
					val := strings.TrimSpace(strings.TrimPrefix(line, "observing_method:"))
					if opt, ok := observingMethodValToOpt[val]; ok {
						observingMethodSelect.SetSelected(opt)
					}
					continue
				}
				if strings.HasPrefix(line, "time_source:") {
					val := strings.TrimSpace(strings.TrimPrefix(line, "time_source:"))
					if opt, ok := timeSourceValToOpt[val]; ok {
						timeSourceSelect.SetSelected(opt)
					}
					continue
				}
				if strings.HasPrefix(line, "camera:") {
					cameraEntry.SetText(strings.TrimSpace(strings.TrimPrefix(line, "camera:")))
					continue
				}
			}

			if serr := scanner.Err(); serr != nil {
				suppressWriteSiteEnable = false
				dialog.ShowError(fmt.Errorf("error reading site file: %w", serr), w)
				return
			}

			suppressWriteSiteEnable = false
			writeSiteBtn.Disable()
			lastLoadedSitePath = filePath
			logAction(fmt.Sprintf("Site file loaded: %s", filePath))
		}, w)

		fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".site"}))
		fileDialog.Resize(fyne.NewSize(800, 600))

		siteDir := filepath.Join(appDir, "SITE-FILES")
		if merr := os.MkdirAll(siteDir, 0755); merr != nil {
			dialog.ShowError(fmt.Errorf("failed to create SITE-FILES directory: %w", merr), w)
			return
		}
		folderURI := storage.NewFileURI(siteDir)
		if listableURI, lerr := storage.ListerForURI(folderURI); lerr == nil {
			fileDialog.SetLocation(listableURI)
		}

		fileDialog.Show()
	})
	loadSiteBtn.Importance = widget.HighImportance

	// --- Write site file button ---
	writeSiteBtn = widget.NewButton("Write site file", func() {
		saveDialog := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if writer == nil {
				return // User cancelled
			}

			// Enforce .site extension
			originalPath := writer.URI().Path()
			filePath := originalPath
			ext := filepath.Ext(filePath)
			if strings.ToLower(ext) != ".site" {
				filePath = strings.TrimSuffix(filePath, ext) + ".site"
			}

			// Close the writer first so the file handle is released
			if cerr := writer.Close(); cerr != nil {
				dialog.ShowError(fmt.Errorf("failed to close file: %w", cerr), w)
			}

			// Remove the empty file created by the save dialog if the path changed
			if filePath != originalPath {
				if rerr := os.Remove(originalPath); rerr != nil {
					fmt.Printf("Warning: could not remove empty file %s: %v\n", originalPath, rerr)
				}
			}

			var sb strings.Builder
			sb.WriteString("latitude_decimal: " + strings.TrimSpace(latDecimalEntry.Text) + "\n")
			sb.WriteString("longitude_decimal: " + strings.TrimSpace(longDecimalEntry.Text) + "\n")
			sb.WriteString("altitude: " + strings.TrimSpace(altitudeEntry.Text) + "\n")
			sb.WriteString("observer1: " + strings.TrimSpace(observer1Entry.Text) + "\n")
			sb.WriteString("observer2: " + strings.TrimSpace(observer2Entry.Text) + "\n")
			sb.WriteString("observatory: " + strings.TrimSpace(observatoryEntry.Text) + "\n")
			sb.WriteString("email: " + strings.TrimSpace(emailEntry.Text) + "\n")
			sb.WriteString("address: " + strings.TrimSpace(addressEntry.Text) + "\n")
			sb.WriteString("nearest_city: " + strings.TrimSpace(nearestCityEntry.Text) + "\n")
			sb.WriteString("country_code: " + strings.TrimSpace(countryCodeEntry.Text) + "\n")
			sb.WriteString("telescope: " + telescopeOptToVal[telescopeSelect.Selected] + "\n")
			sb.WriteString("aperture: " + strings.TrimSpace(apertureEntry.Text) + "\n")
			sb.WriteString("focal_length: " + strings.TrimSpace(focalLengthEntry.Text) + "\n")
			sb.WriteString("observing_method: " + observingMethodOptToVal[observingMethodSelect.Selected] + "\n")
			sb.WriteString("time_source: " + timeSourceOptToVal[timeSourceSelect.Selected] + "\n")
			sb.WriteString("camera: " + strings.TrimSpace(cameraEntry.Text) + "\n")
			siteContent := sb.String()

			if werr := os.WriteFile(filePath, []byte(siteContent), 0644); werr != nil {
				dialog.ShowError(fmt.Errorf("failed to write site file: %w", werr), w)
				return
			}
			lastLoadedSitePath = filePath

			// Populate SODIS/VizieR fields the same way Load site does
			if value := strings.TrimSpace(latDecimalEntry.Text); value != "" {
				if lat, perr := strconv.ParseFloat(value, 64); perr == nil {
					deg, minutes, sec := decimalToDMS(lat)
					vt.SiteLatDegEntry.SetText(deg)
					vt.SiteLatMinEntry.SetText(minutes)
					vt.SiteLatSecsEntry.SetText(sec)
				}
			}
			if value := strings.TrimSpace(longDecimalEntry.Text); value != "" {
				if lon, perr := strconv.ParseFloat(value, 64); perr == nil {
					deg, minutes, sec := decimalToDMS(lon)
					vt.SiteLongDegEntry.SetText(deg)
					vt.SiteLongMinEntry.SetText(minutes)
					vt.SiteLongSecsEntry.SetText(sec)
				}
			}
			if value := strings.TrimSpace(altitudeEntry.Text); value != "" {
				vt.SiteAltitudeEntry.SetText(value)
			}
			if value := strings.TrimSpace(observer1Entry.Text); value != "" {
				vt.ObserverNameEntry.SetText(value)
			}

			logAction(fmt.Sprintf("Site file written: %s", filePath))
		}, w)

		saveDialog.SetFileName("")
		saveDialog.Resize(fyne.NewSize(800, 600))

		siteDir := filepath.Join(appDir, "SITE-FILES")
		if merr := os.MkdirAll(siteDir, 0755); merr != nil {
			dialog.ShowError(fmt.Errorf("failed to create SITE-FILES directory: %w", merr), w)
			return
		}
		folderURI := storage.NewFileURI(siteDir)
		if listableURI, lerr := storage.ListerForURI(folderURI); lerr == nil {
			saveDialog.SetLocation(listableURI)
		}

		saveDialog.Show()
	})
	writeSiteBtn.Importance = widget.HighImportance
	writeSiteBtn.Disable()

	// Enable writeSiteBtn only on user-driven edits (not programmatic SetText).
	enableWriteSite := func(_ string) {
		if !suppressWriteSiteEnable && writeSiteBtn.Disabled() {
			writeSiteBtn.Enable()
		}
	}
	for _, e := range []*widget.Entry{
		latDecimalEntry, longDecimalEntry, altitudeEntry,
		observer1Entry, observer2Entry, observatoryEntry,
		emailEntry, addressEntry, nearestCityEntry, countryCodeEntry,
		apertureEntry, focalLengthEntry, cameraEntry,
	} {
		prev := e.OnChanged
		if prev != nil {
			origPrev := prev
			e.OnChanged = func(s string) {
				origPrev(s)
				enableWriteSite(s)
			}
		} else {
			e.OnChanged = enableWriteSite
		}
	}

	// --- Calculate observer dX dY button ---
	var occelmntDialog dialog.Dialog
	calcDxDyBtn := widget.NewButton("Create Occultation Parameter file", func() {
		xmlContent := strings.TrimSpace(pasteEntry.Text)
		if xmlContent == "" {
			dialog.ShowError(fmt.Errorf("please paste occelmnt XML content first"), w)
			return
		}
		lastLoadedOccelmntXml = xmlContent
		fyne.CurrentApp().Preferences().SetString("lastLoadedOccelmntXml", lastLoadedOccelmntXml)
		vt.FillStarFromOccelmntXml(xmlContent)
		lat, err := strconv.ParseFloat(strings.TrimSpace(latDecimalEntry.Text), 64)
		if err != nil {
			dialog.ShowError(fmt.Errorf("invalid latitude (degrees): %v", err), w)
			return
		}
		lon, err := strconv.ParseFloat(strings.TrimSpace(longDecimalEntry.Text), 64)
		if err != nil {
			dialog.ShowError(fmt.Errorf("invalid longitude (degrees): %v", err), w)
			return
		}
		alt := 0.0
		if strings.TrimSpace(altitudeEntry.Text) != "" {
			alt, err = strconv.ParseFloat(strings.TrimSpace(altitudeEntry.Text), 64)
			if err != nil {
				dialog.ShowError(fmt.Errorf("invalid altitude: %v", err), w)
				return
			}
		}

		vx, vy, calcErr := ShadowVelocityFromOWCEventKmPerSec(xmlContent, lat, lon, alt, 0.0, 0.0, 0.0)
		if calcErr != nil {
			dialog.ShowError(fmt.Errorf("calculation error: %v", calcErr), w)
			return
		}

		_, _, _, t0Err := ObserverT0CorrectionFromOWC(xmlContent, lat, lon, alt, 0.0, 0.0, 0.0)

		// Persist the observer location so the SODIS fill can use it even after app restart.
		if t0Err == nil {
			lastObserverLatDeg = lat
			lastObserverLonDeg = lon
			lastObserverAltMeters = alt
			lastObserverLocationSet = true
			p := fyne.CurrentApp().Preferences()
			p.SetFloat("lastObserverLatDeg", lat)
			p.SetFloat("lastObserverLonDeg", lon)
			p.SetFloat("lastObserverAltMeters", alt)
			p.SetBool("lastObserverLocationSet", true)
		}

		// Parse <Object> or <object> for distance_au (index 4) and body diameter (index 3)
		var occ Occultations
		if xmlErr := xml.Unmarshal([]byte(xmlContent), &occ); xmlErr != nil {
			dialog.ShowError(fmt.Errorf("failed to parse XML for Object: %v", xmlErr), w)
			return
		}
		distanceAu := 0.0
		bodyDiamKm := 0.0
		titleStr := ""
		if len(occ.Events) > 0 {
			objectText := occ.Events[0].Object
			if objectText == "" {
				objectText = occ.Events[0].ObjectLC
			}
			if objectText != "" {
				objFields := splitCSVPreserveEmpty(objectText)
				if len(objFields) > 4 {
					if d, derr := strconv.ParseFloat(objFields[4], 64); derr == nil {
						distanceAu = d
					}
				}
				if len(objFields) > 3 {
					if d, derr := strconv.ParseFloat(objFields[3], 64); derr == nil {
						bodyDiamKm = d
					}
				}
				if len(objFields) > 1 {
					titleStr = fmt.Sprintf("(%s) %s", objFields[0], objFields[1])
				}
			}
		}

		// Build a parameters struct with the computed values
		params := OccultationParameters{
			Title:                          titleStr,
			WindowSizePixels:               600,
			FundamentalPlaneWidthKm:        math.Ceil(5 * bodyDiamKm),
			FundamentalPlaneWidthNumPoints: 2000,
			DXKmPerSec:                     vx,
			DYKmPerSec:                     vy,
			DistanceAu:                     distanceAu,
			ObservationWavelengthNm:        550,
			MainBody: EllipseParams{
				MajorAxisKm: bodyDiamKm,
				MinorAxisKm: bodyDiamKm,
			},
			OccelmntXml: xmlContent,
		}

		// Write a copy of the site data into the -RESULTS folder if available.
		if resultsFolder != "" {
			var sb strings.Builder
			sb.WriteString("latitude_decimal: " + strings.TrimSpace(latDecimalEntry.Text) + "\n")
			sb.WriteString("longitude_decimal: " + strings.TrimSpace(longDecimalEntry.Text) + "\n")
			sb.WriteString("altitude: " + strings.TrimSpace(altitudeEntry.Text) + "\n")
			sb.WriteString("observer1: " + strings.TrimSpace(observer1Entry.Text) + "\n")
			sb.WriteString("observer2: " + strings.TrimSpace(observer2Entry.Text) + "\n")
			sb.WriteString("observatory: " + strings.TrimSpace(observatoryEntry.Text) + "\n")
			sb.WriteString("email: " + strings.TrimSpace(emailEntry.Text) + "\n")
			sb.WriteString("address: " + strings.TrimSpace(addressEntry.Text) + "\n")
			sb.WriteString("nearest_city: " + strings.TrimSpace(nearestCityEntry.Text) + "\n")
			sb.WriteString("country_code: " + strings.TrimSpace(countryCodeEntry.Text) + "\n")
			sb.WriteString("telescope: " + telescopeOptToVal[telescopeSelect.Selected] + "\n")
			sb.WriteString("aperture: " + strings.TrimSpace(apertureEntry.Text) + "\n")
			sb.WriteString("focal_length: " + strings.TrimSpace(focalLengthEntry.Text) + "\n")
			sb.WriteString("observing_method: " + observingMethodOptToVal[observingMethodSelect.Selected] + "\n")
			sb.WriteString("time_source: " + timeSourceOptToVal[timeSourceSelect.Selected] + "\n")
			sb.WriteString("camera: " + strings.TrimSpace(cameraEntry.Text) + "\n")

			sitePath := filepath.Join(resultsFolder, "site_data.site")
			if werr := os.WriteFile(sitePath, []byte(sb.String()), 0644); werr != nil {
				fmt.Printf("Warning: could not write site copy to results folder: %v\n", werr)
			} else {
				logAction(fmt.Sprintf("Site data copied to: %s", sitePath))
			}
		}

		// Close this dialog and open the parameters dialog pre-populated from the computed values
		if occelmntDialog != nil {
			occelmntDialog.Hide()
		}
		obsDir := ""
		if loadedLightCurveData != nil && loadedLightCurveData.SourceFilePath != "" {
			obsDir = filepath.Dir(loadedLightCurveData.SourceFilePath)
		}
		showOccultationParametersDialog(w, false, &params, obsDir)

		// Calculate and display t0 correction and Fresnel scale
		wavelength := float64(params.ObservationWavelengthNm)
		if wavelength == 0 {
			wavelength = 550
		}
		infoMsg := ""
		if params.DistanceAu > 0 {
			fresnelKm := FresnelScale(wavelength, params.DistanceAu)
			fresnelM := fresnelKm * 1000
			infoMsg += fmt.Sprintf("Fresnel scale: %.4f km (%.1f meters)\n\nWavelength: %.0f nm\nDistance: %.4f AU",
				fresnelKm, fresnelM, wavelength, params.DistanceAu)
			if params.FundamentalPlaneWidthKm > 0 && params.FundamentalPlaneWidthNumPoints > 0 {
				samplesPerFresnel := int(float64(params.FundamentalPlaneWidthNumPoints) * fresnelKm / params.FundamentalPlaneWidthKm)
				infoMsg += fmt.Sprintf("\n\nSamples per Fresnel scale: %d", samplesPerFresnel)
			}
			infoMsg += "\n\nIf your observation exhibits diffraction effects (sloped D and R transitions), " +
				"you will need the Samples per Fresnel scale to be 5 or 6 at a minimum. " +
				"See the Help Topics entry titled 'Fresnel scale resolution' for more information."
		}
		if infoMsg != "" {
			dialog.ShowInformation("Fresnel Scale Sampling Report", infoMsg, w)
		}
	})
	calcDxDyBtn.Importance = widget.HighImportance

	cancelBtn := widget.NewButton("Cancel", func() {
		if occelmntDialog != nil {
			occelmntDialog.Hide()
		}
	})
	cancelBtn.Importance = widget.HighImportance

	// --- Assemble bottom sections ---
	bottomSection := container.NewVBox(
		widget.NewSeparator(),
		observerEquipSection,
		widget.NewSeparator(),
		siteLocationSection,
		widget.NewSeparator(),
		container.NewHBox(writeSiteBtn),
		widget.NewSeparator(),
		container.NewHBox(cancelBtn),
	)

	loadOccelmntBtn.Importance = widget.HighImportance
	pasteSection := container.NewBorder(container.NewHBox(loadOccelmntBtn), container.NewHBox(loadSiteBtn, calcDxDyBtn, layout.NewSpacer()), nil, nil, scrollable)
	content := container.NewBorder(nil, bottomSection, nil, nil, pasteSection)

	occelmntDialog = dialog.NewCustomWithoutButtons("Process OWC Occelmnt", content, w)
	occelmntDialog.Resize(fyne.NewSize(840, 1000))
	occelmntDialog.Show()

	// Focus the entry so the user can immediately paste
	w.Canvas().Focus(pasteEntry)
}
