package main

import (
	"GoPyOTE/lightcurve"
	"bytes"
	"encoding/csv"
	"fmt"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// buildFitTab constructs the Fit tab.
func buildFitTab(ac *appContext) *container.TabItem {
	w := ac.window
	a := ac.app
	tab10Bg := ac.makeTabBg(color.RGBA{R: 200, G: 220, B: 240, A: 255}, color.RGBA{R: 50, G: 70, B: 90, A: 255})

	// Status label for Fit tab
	fitStatusLabel := widget.NewLabel("Select pairs of points to define baseline regions")

	// Stored baseline noise sigma for Monte Carlo and NIE
	var noiseSigma float64
	var baselineValues []float64 // baseline region values (after scaling to unity)
	var baselineIndices []int    // corresponding indices into loadedLightCurveData
	var rho []float64            // pre-detrend autocorrelation coefficients, rho[0]=1.0, out to lag 10

	// Stored last fit result, params, candidate curves, and target data for Monte Carlo
	var lastFitResult *fitResult
	var lastFitParams *OccultationParameters
	var lastFitCandidates []*precomputedCurve
	var lastFitBestIdx int // index into lastFitCandidates of the best path offset
	var lastFitTargetTimes []float64
	var lastMCResult *mcTrialsResult // most recent successful Monte Carlo run

	mcNarrowSearchCheck := widget.NewCheck("Narrow MC offset search (±20 steps)", nil)
	mcNarrowSearchCheck.Checked = true

	nieSinglePointCheck := widget.NewCheck("Enable NIE point(s) selection", func(checked bool) {
		ac.nieManualSelectMode = checked
		if checked {
			// Switch to regular two-point mode so clicks set Point1/Point2 directly
			// instead of creating baseline pairs.
			ac.lightCurvePlot.SingleSelectMode = false
			ac.lightCurvePlot.MultiPairSelectMode = false
			// Clear any pending point selections so the user starts fresh.
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
		} else {
			// Restore multi-pair mode for baseline region selection.
			ac.lightCurvePlot.MultiPairSelectMode = true
			ac.lightCurvePlot.Refresh()
		}
	})
	nieSinglePointCheck.Checked = false

	// Calculate Baseline mean button: computes mean, extracts noise, scales to unity
	var calcBaselineMeanBtn *widget.Button
	calcBaselineMeanBtn = widget.NewButton("Normalize baseline and estimate noise sigma (used for Monte Carlo and NIE trials)", func() {
		if len(ac.lightCurvePlot.SelectedPairs) == 0 {
			dialog.ShowError(fmt.Errorf("no point pairs selected - click on points to select baseline regions"), w)
			return
		}
		if loadedLightCurveData == nil {
			dialog.ShowError(fmt.Errorf("no light curve data loaded"), w)
			return
		}

		// Collect all points from all pairs and calculate the average
		var sum float64
		var count int

		for _, pair := range ac.lightCurvePlot.SelectedPairs {
			idx1 := pair.Point1DataIdx
			idx2 := pair.Point2DataIdx
			if idx1 > idx2 {
				idx1, idx2 = idx2, idx1
			}

			var col *LightCurveColumn
			for i := range loadedLightCurveData.Columns {
				if loadedLightCurveData.Columns[i].Name == pair.Point1Series {
					col = &loadedLightCurveData.Columns[i]
					break
				}
			}
			if col == nil {
				continue
			}

			for i := idx1; i <= idx2 && i < len(col.Values); i++ {
				sum += col.Values[i]
				count++
			}
		}

		if count == 0 {
			dialog.ShowError(fmt.Errorf("no valid points found in selected pairs"), w)
			return
		}

		mean := sum / float64(count)
		logAction(fmt.Sprintf("Fit: Calculated baseline mean = %.4f from %d points in %d pairs", mean, count, len(ac.lightCurvePlot.SelectedPairs)))

		if mean == 0 {
			dialog.ShowError(fmt.Errorf("baseline mean is zero - cannot scale"), w)
			return
		}

		// Extract noise before scaling: noise = value/mean - 1.0 (equivalent to (value - mean)/mean)
		var noise []float64
		var noiseDataIndices []int // track original data indices for trend plotting
		for _, pair := range ac.lightCurvePlot.SelectedPairs {
			idx1 := pair.Point1DataIdx
			idx2 := pair.Point2DataIdx
			if idx1 > idx2 {
				idx1, idx2 = idx2, idx1
			}

			var col *LightCurveColumn
			for i := range loadedLightCurveData.Columns {
				if loadedLightCurveData.Columns[i].Name == pair.Point1Series {
					col = &loadedLightCurveData.Columns[i]
					break
				}
			}
			if col == nil {
				continue
			}

			for i := idx1; i <= idx2 && i < len(col.Values); i++ {
				noise = append(noise, col.Values[i]/mean-1.0)
				noiseDataIndices = append(noiseDataIndices, i)
			}
		}
		// Store baseline values (scaled to unity) and their data indices.
		baselineValues = make([]float64, len(noise))
		baselineIndices = make([]int, len(noise))
		for i, n := range noise {
			baselineValues[i] = n + 1.0
			baselineIndices[i] = noiseDataIndices[i]
		}

		// Scale all column values to unity
		scaleFactor := mean
		logAction(fmt.Sprintf("Fit: Scaling all light curves by 1/%.4f to set baseline mean to unity", scaleFactor))
		for colIdx := range loadedLightCurveData.Columns {
			for i := range loadedLightCurveData.Columns[colIdx].Values {
				loadedLightCurveData.Columns[colIdx].Values[i] /= scaleFactor
			}
		}

		ac.lightCurvePlot.BaselineValue = 1.0
		ac.lightCurvePlot.ShowBaselineLine = true
		ac.lightCurvePlot.SelectedPairs = nil
		baselineScaledToUnity = true

		// Compute autocorrelation coefficients and fit an AR model for correlated noise.
		if len(baselineValues) >= 5 {
			preLagMax := 10
			if len(noise) <= preLagMax {
				preLagMax = len(noise) - 1
			}
			if preLagMax >= 1 {
				preLagCoeffs, preLagErr := autocorrCoeffs(noise, preLagMax)
				if preLagErr != nil {
					fmt.Printf("Autocorrelation failed: %v\n", preLagErr)
				} else {
					// Build rho[0..10] with rho[0]=1.0 for use by testARmethod.
					rho = make([]float64, len(preLagCoeffs)+1)
					rho[0] = 1.0
					copy(rho[1:], preLagCoeffs)

					// Log lag coefficients.
					lagStrs := make([]string, len(preLagCoeffs))
					for i, v := range preLagCoeffs {
						lagStrs[i] = fmt.Sprintf("lag%d=%0.4f", i+1, v)
					}
					logAction(fmt.Sprintf("Lag coefficients: %s", strings.Join(lagStrs, " ")))

					// Fit AR model from rho and store phi/sigma2 for correlated noise generation.
					arOrder := len(preLagCoeffs)
					phi, sigma2, arErr := fitARFromACF(rho, arOrder)
					if arErr != nil {
						fmt.Printf("AR model fit failed: %v\n", arErr)
						ac.arPhi = nil
						ac.arSigma2 = 0
					} else {
						ac.arPhi = phi
						ac.arSigma2 = sigma2
						logAction(fmt.Sprintf("Fit: AR(%d) model fitted, innovation variance=%.6f", arOrder, sigma2))
					}

					fmt.Printf("\nBaseline noise (sigma=%.10f):\n", stddev(noise))
					fmt.Println("Lag autocorrelation coefficients:")
					for i, v := range rho {
						fmt.Printf("  lag %d: %.10f\n", i, v)
					}
				}
			}
		}

		// Rebuild the plot after the trend series is set so the trend line is visible.
		savedMinY, savedMaxY := ac.lightCurvePlot.GetYBounds()
		ac.rebuildPlot(func() {
			ac.lightCurvePlot.SetYBounds(savedMinY/scaleFactor, savedMaxY/scaleFactor)
		})

		// Show noise histogram if we have enough points and diagnostics are enabled
		if len(noise) >= 2 {
			histImg, noiseMean, sigma, err := createNoiseHistogramImage(noise, lastDiffractionTitle, 800, 500)
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to create noise histogram: %v", err), w)
			} else {
				noiseSigma = sigma
				if ac.showDiagnostics {
					histWindow := a.NewWindow("Baseline Noise Histogram")
					histCanvas := canvas.NewImageFromImage(histImg)
					histCanvas.FillMode = canvas.ImageFillOriginal
					histWindow.SetContent(container.NewScroll(histCanvas))
					histWindow.Resize(fyne.NewSize(850, 550))
					histWindow.CenterOnScreen()
					histWindow.Show()
				}
				logAction(fmt.Sprintf("Fit: Extracted baseline noise: %d points, mean=%.6f, histogram sigma=%.6f", len(noise), noiseMean, sigma))
			}
		}

		fitStatusLabel.SetText(fmt.Sprintf("Scaled to unity (baseline=%.4f, %d points) — noise sigma=%.6f", mean, count, noiseSigma))
		calcBaselineMeanBtn.Importance = widget.WarningImportance
		calcBaselineMeanBtn.Refresh()
	})
	calcBaselineMeanBtn.Importance = widget.HighImportance
	ac.resetNormalizeBtn = func() {
		calcBaselineMeanBtn.Importance = widget.HighImportance
		calcBaselineMeanBtn.Refresh()
	}

	// Search range for observation path offset
	searchInitialOffsetEntry := NewFocusLossEntry()
	searchInitialOffsetEntry.SetPlaceHolder("")
	searchFinalOffsetEntry := NewFocusLossEntry()
	searchFinalOffsetEntry.SetPlaceHolder("")
	searchNumStepsEntry := widget.NewEntry()
	searchNumStepsEntry.SetPlaceHolder("")

	showSearchRangeHelp := func() {
		dialog.ShowInformation("Search range for observation path offsets",
			"You may wish to narrow the search range after the initial full range search has completed because the Monte Carlo process will then take less time.\n\nRemember to click Run Fit Search after making changes to the search range because the Monte Carlo trials use the path range from the last click on Run Fit Search.", w)
	}

	// updateSearchNumSteps recalculates the number of steps from the current offset
	// range and the pixel resolution of the diffraction image parameters file.
	updateSearchNumSteps := func() {
		initVal, err1 := strconv.ParseFloat(strings.TrimSpace(searchInitialOffsetEntry.Text), 64)
		finalVal, err2 := strconv.ParseFloat(strings.TrimSpace(searchFinalOffsetEntry.Text), 64)
		if err1 != nil || err2 != nil || lastDiffractionParamsPath == "" {
			return
		}
		go func() {
			file, err := os.Open(lastDiffractionParamsPath)
			if err != nil {
				return
			}
			params, err := parseOccultationParameters(file)
			if closeErr := file.Close(); closeErr != nil {
				fmt.Printf("Failed to close parameters file: %v\n", closeErr)
			}
			if err != nil || params.FundamentalPlaneWidthNumPoints == 0 {
				return
			}
			stepSize := params.FundamentalPlaneWidthKm / float64(params.FundamentalPlaneWidthNumPoints)
			if stepSize <= 0 {
				return
			}
			numSteps := int(math.Abs(finalVal-initVal)/stepSize) + 1
			fyne.Do(func() {
				searchNumStepsEntry.SetText(fmt.Sprintf("%d", numSteps))
			})
		}()
	}

	var showSearchRangeBtn *widget.Button
	suppressSearchRangeEnable := false
	searchInitialOffsetEntry.OnSubmitted = func(_ string) { updateSearchNumSteps() }
	searchFinalOffsetEntry.OnSubmitted = func(_ string) { updateSearchNumSteps() }
	searchInitialOffsetEntry.OnChanged = func(_ string) {
		if ac.resetFitButtons != nil {
			ac.resetFitButtons()
		}
		if !suppressSearchRangeEnable && showSearchRangeBtn != nil {
			showSearchRangeBtn.Enable()
		}
	}
	searchFinalOffsetEntry.OnChanged = func(_ string) {
		if ac.resetFitButtons != nil {
			ac.resetFitButtons()
		}
		if !suppressSearchRangeEnable && showSearchRangeBtn != nil {
			showSearchRangeBtn.Enable()
		}
	}

	// Register callback so main.go's tabs.OnSelected can autofill search range
	ac.autoFillSearchRange = func() {
		if lastDiffractionParamsPath == "" ||
			strings.TrimSpace(searchInitialOffsetEntry.Text) != "" ||
			strings.TrimSpace(searchFinalOffsetEntry.Text) != "" {
			return
		}
		go func() {
			file, err := os.Open(lastDiffractionParamsPath)
			if err != nil {
				return
			}
			params, err := parseOccultationParameters(file)
			if closeErr := file.Close(); closeErr != nil {
				fmt.Printf("Failed to close parameters file: %v\n", closeErr)
			}
			if err != nil {
				return
			}
			finalVal := params.MainBody.MajorAxisKm / 2
			if params.PathToExternalImage != "" && finalVal == 0 {
				finalVal = params.FundamentalPlaneWidthKm / 2
			}
			numSteps := 0
			if params.FundamentalPlaneWidthNumPoints > 0 {
				stepSize := params.FundamentalPlaneWidthKm / float64(params.FundamentalPlaneWidthNumPoints)
				if stepSize > 0 {
					numSteps = int(math.Abs(finalVal)/stepSize) + 1
				}
			}
			ns := numSteps
			fv := finalVal
			fyne.Do(func() {
				suppressSearchRangeEnable = true
				searchInitialOffsetEntry.SetText("0.0")
				searchFinalOffsetEntry.SetText(strconv.FormatFloat(fv, 'f', -1, 64))
				if ns > 0 {
					searchNumStepsEntry.SetText(fmt.Sprintf("%d", ns))
				}
				suppressSearchRangeEnable = false
			})
		}()
	}

	// Preview window for search range paths — kept so we can update in place
	var searchPreviewWindow fyne.Window

	showSearchRangePreview := func() {
		initText := strings.TrimSpace(searchInitialOffsetEntry.Text)
		finalText := strings.TrimSpace(searchFinalOffsetEntry.Text)
		if initText == "" || finalText == "" {
			return
		}
		initVal, err1 := strconv.ParseFloat(initText, 64)
		finalVal, err2 := strconv.ParseFloat(finalText, 64)
		if err1 != nil || err2 != nil {
			return
		}
		if lastDiffractionParamsPath == "" {
			return
		}
		go func() {
			file, err := os.Open(lastDiffractionParamsPath)
			if err != nil {
				return
			}
			params, err := parseOccultationParameters(file)
			if closeErr := file.Close(); closeErr != nil {
				fmt.Printf("Failed to close parameters file: %v\n", closeErr)
			}
			if err != nil {
				return
			}

			// Auto-calculate the number of steps from image resolution
			if params.FundamentalPlaneWidthNumPoints > 0 {
				stepSize := params.FundamentalPlaneWidthKm / float64(params.FundamentalPlaneWidthNumPoints)
				if stepSize > 0 {
					numSteps := int(math.Abs(finalVal-initVal)/stepSize) + 1
					fyne.Do(func() {
						searchNumStepsEntry.SetText(fmt.Sprintf("%d", numSteps))
					})
				}
			}
			baseImg, err := lightcurve.LoadImageFromFile(filepath.Join(appDir, "diffractionImage8bit.png"))
			if err != nil {
				return
			}
			fundPlaneWidthPts := params.FundamentalPlaneWidthNumPoints
			if params.PathToExternalImage != "" {
				fundPlaneWidthPts = baseImg.Bounds().Dx()
			}
			// Draw the initial offset path
			path1 := &lightcurve.ObservationPath{
				DxKmPerSec:               params.DXKmPerSec,
				DyKmPerSec:               params.DYKmPerSec,
				PathOffsetFromCenterKm:   initVal,
				FundamentalPlaneWidthKm:  params.FundamentalPlaneWidthKm,
				FundamentalPlaneWidthPts: fundPlaneWidthPts,
			}
			if err := path1.ComputePathFromVelocity(); err != nil {
				return
			}
			annotatedImg, err := lightcurve.DrawObservationLineOnImage(baseImg, path1)
			if err != nil {
				return
			}
			// Draw the final offset path on the same image
			path2 := &lightcurve.ObservationPath{
				DxKmPerSec:               params.DXKmPerSec,
				DyKmPerSec:               params.DYKmPerSec,
				PathOffsetFromCenterKm:   finalVal,
				FundamentalPlaneWidthKm:  params.FundamentalPlaneWidthKm,
				FundamentalPlaneWidthPts: fundPlaneWidthPts,
			}
			if err := path2.ComputePathFromVelocity(); err != nil {
				return
			}
			annotatedImg, err = lightcurve.DrawObservationLineOnImage(annotatedImg, path2)
			if err != nil {
				return
			}
			// Save the search range image to the results folder
			if resultsFolder != "" {
				var buf bytes.Buffer
				if err := png.Encode(&buf, annotatedImg); err == nil {
					savePath := filepath.Join(resultsFolder, "searchRange.png")
					if err := os.WriteFile(savePath, buf.Bytes(), 0644); err != nil {
						fmt.Printf("Warning: could not save searchRange.png: %v\n", err)
					}
				}
			}
			fyne.Do(func() {
				if searchPreviewWindow != nil {
					searchPreviewWindow.Close()
				}
				previewTitle := fmt.Sprintf("Search Range: %.3f to %.3f km", initVal, finalVal)
				if lastDiffractionTitle != "" {
					previewTitle = lastDiffractionTitle + " — " + previewTitle
				}
				searchPreviewWindow = a.NewWindow(previewTitle)
				previewCanvas := canvas.NewImageFromImage(annotatedImg)
				previewCanvas.FillMode = canvas.ImageFillContain
				searchPreviewWindow.SetContent(previewCanvas)
				searchPreviewWindow.Resize(fyne.NewSize(600, 600))
				searchPreviewWindow.CenterOnScreen()
				searchPreviewWindow.Show()
			})
		}()
	}

	searchRangeForm := widget.NewForm(
		&widget.FormItem{Text: "Initial offset", Widget: searchInitialOffsetEntry},
		&widget.FormItem{Text: "Final offset", Widget: searchFinalOffsetEntry},
		&widget.FormItem{Text: "Number of steps", Widget: searchNumStepsEntry},
	)

	showSearchRangeBtn = widget.NewButton("Show search range", func() {
		showSearchRangePreview()
	})
	showSearchRangeBtn.Importance = widget.HighImportance
	showSearchRangeBtn.Disable()

	searchRangeTitle := widget.NewLabel("Search range for observation path offset")
	searchRangeTitle.TextStyle = fyne.TextStyle{Bold: true}
	searchRangeTitleRow := container.NewBorder(nil, nil, nil, showSearchRangeBtn, searchRangeTitle)
	searchRangeCard := widget.NewCard("", "", container.NewVBox(searchRangeTitleRow, searchRangeForm))

	fitProgressBar := widget.NewProgressBar()
	fitProgressBar.Hide()

	// Fit abort button
	var fitAbortFlag atomic.Bool
	var fitAbortBtn *widget.Button
	fitAbortBtn = widget.NewButton("Abort", func() {
		fitAbortFlag.Store(true)
		fitAbortBtn.Disable()
	})
	fitAbortBtn.Importance = widget.DangerImportance
	fitAbortBtn.Hide()

	// Fit button - checks preconditions and reports readiness
	var fitBtn *widget.Button
	fitBtn = widget.NewButton("Run fit search", func() {
		var issues []string

		// Check 1: Single curve selected
		if len(ac.displayedCurves) != 1 {
			issues = append(issues, fmt.Sprintf("A single light curve must be selected (currently %d displayed)", len(ac.displayedCurves)))
		}

		// Check 2: Scaled to unity
		if !baselineScaledToUnity {
			issues = append(issues, "Light curve has not been scaled to unity")
		}

		// Check 3: Parameters file from IOTAdiffraction run
		if lastDiffractionParamsPath == "" {
			issues = append(issues, "No diffraction has been generated (run IOTAdiffraction first)")
		}

		// Check 4: Diffraction image available
		if _, err := os.Stat(filepath.Join(appDir, "targetImage16bit.png")); os.IsNotExist(err) {
			issues = append(issues, "No diffraction image available (targetImage16bit.png not found)")
		}

		if len(issues) > 0 {
			msg := "Cannot perform fit. The following conditions are not met:\n\n"
			for i, issue := range issues {
				msg += fmt.Sprintf("%d. %s\n", i+1, issue)
			}
			dialog.ShowError(fmt.Errorf("%s", msg), w)
		} else {
			runFitBody := func() {
				// Clear previous fit results so edges don't accumulate across runs
				lastFitResult = nil
				lastFitParams = nil
				lastFitCandidates = nil
				lastFitTargetTimes = nil
				ac.theorySeries = nil
				ac.trendSeries = nil
				ac.lightCurvePlot.SetVerticalLines(nil, false)
				ac.lightCurvePlot.SetSigmaLines(nil, false)

				// Load parameters from the file used to generate the diffraction image
				file, err := os.Open(lastDiffractionParamsPath)
				if err != nil {
					dialog.ShowError(fmt.Errorf("could not open parameters file: %v", err), w)
					return
				}
				params, err := parseOccultationParameters(file)
				if closeErr := file.Close(); closeErr != nil {
					dialog.ShowError(fmt.Errorf("failed to close file: %w", closeErr), w)
				}
				if err != nil {
					dialog.ShowError(fmt.Errorf("could not parse parameters: %v", err), w)
					return
				}

				// Find the single displayed column index
				var displayedColIdx int
				for k := range ac.displayedCurves {
					displayedColIdx = k
					break
				}

				// Verify timestamps are real (not all zeros)
				allZero := true
				for _, t := range loadedLightCurveData.TimeValues {
					if t != 0 {
						allZero = false
						break
					}
				}
				if allZero {
					dialog.ShowError(fmt.Errorf("timestamps are all zero — real timestamps are required for fitting"), w)
					return
				}

				// Collect target times and values within the frame range
				col := loadedLightCurveData.Columns[displayedColIdx]
				var targetTimes, targetValues []float64
				for i, val := range col.Values {
					frameNum := loadedLightCurveData.FrameNumbers[i]
					if ac.frameRangeStart > 0 && frameNum < ac.frameRangeStart {
						continue
					}
					if ac.frameRangeEnd > 0 && frameNum > ac.frameRangeEnd {
						continue
					}
					targetTimes = append(targetTimes, loadedLightCurveData.TimeValues[i])
					targetValues = append(targetValues, val)
				}

				if len(targetTimes) < 2 {
					dialog.ShowError(fmt.Errorf("not enough data points in displayed range for fitting"), w)
					return
				}

				params.ExposureTimeSecs = lastCsvExposureSecs
				if lastCsvExposureSecs == 0 {
					logAction("Fit: camera exposure time not set (0 seconds)")
				} else {
					logAction(fmt.Sprintf("Fit: camera exposure time: %.6f seconds", lastCsvExposureSecs))
				}
				// Check if search range fields are all filled in
				searchInitial := strings.TrimSpace(searchInitialOffsetEntry.Text)
				searchFinal := strings.TrimSpace(searchFinalOffsetEntry.Text)
				searchSteps := strings.TrimSpace(searchNumStepsEntry.Text)

				if searchInitial != "" && searchFinal != "" && searchSteps != "" {
					initVal, err := strconv.ParseFloat(searchInitial, 64)
					if err != nil {
						dialog.ShowError(fmt.Errorf("invalid Initial offset: %v", err), w)
						return
					}
					finalVal, err := strconv.ParseFloat(searchFinal, 64)
					if err != nil {
						dialog.ShowError(fmt.Errorf("invalid Final offset: %v", err), w)
						return
					}
					stepsVal, err := strconv.Atoi(searchSteps)
					if err != nil || stepsVal < 1 {
						dialog.ShowError(fmt.Errorf("number of steps must be a positive integer"), w)
						return
					}
					fitProgressBar.SetValue(0)
					fitProgressBar.Show()
					fitBtn.Disable()
					fitAbortFlag.Store(false)
					fitAbortBtn.Show()
					fitAbortBtn.Enable()
					go func() {
						fsr, err := runFitSearch(params, targetTimes, targetValues, initVal, finalVal, stepsVal, &fitAbortFlag, func(progress float64) {
							fyne.Do(func() {
								fitProgressBar.SetValue(progress)
							})
						})
						fyne.Do(func() {
							fitProgressBar.Hide()
							fitAbortBtn.Hide()
							fitBtn.Enable()
							if err != nil {
								dialog.ShowError(err, w)
							} else {
								fr, err := displayFitSearchResult(a, w, params, fsr, targetTimes, targetValues, ac.showDiagnostics)
								if err != nil {
									dialog.ShowError(err, w)
								} else {
									lastFitResult = fr
									paramsCopy := *params
									lastFitParams = &paramsCopy
									// Save all precomputed curves from the search for Monte Carlo
									lastFitCandidates = make([]*precomputedCurve, 0, len(fsr.results))
									for _, sr := range fsr.results {
										lastFitCandidates = append(lastFitCandidates, sr.pc)
									}
									lastFitBestIdx = fsr.bestIdx
									logAction(fmt.Sprintf("Fit search: %d of %d path offset steps succeeded, %d candidate curves saved for Monte Carlo", len(fsr.results), stepsVal, len(lastFitCandidates)))
									lastFitTargetTimes = targetTimes
									fitBtn.Importance = widget.WarningImportance
									fitBtn.Refresh()
									if ac.enablePostFitButtons != nil {
										ac.enablePostFitButtons()
									}
									ac.overlayTheoryCurve(fr, nil)
								}
							}
						})
					}()
				} else {
					fr, pc, err := performFit(a, w, params, targetTimes, targetValues, ac.showDiagnostics)
					if err != nil {
						dialog.ShowError(err, w)
					} else {
						lastFitResult = fr
						paramsCopy := *params
						lastFitParams = &paramsCopy
						lastFitCandidates = []*precomputedCurve{pc}
						lastFitTargetTimes = targetTimes
						fitBtn.Importance = widget.WarningImportance
						fitBtn.Refresh()
						if ac.enablePostFitButtons != nil {
							ac.enablePostFitButtons()
						}
						ac.overlayTheoryCurve(fr, nil)
					}
				}
			}
			if !trimPerformed {
				noBtn := widget.NewButton("No", nil)
				noBtn.Importance = widget.HighImportance
				yesBtn := widget.NewButton("Yes, run anyway", nil)
				trimDlg := dialog.NewCustom("Trim not set",
					"",
					container.NewVBox(
						widget.NewLabel("A Set trim operation is recommended before running a fit search.\n\nDo you want to run anyway?"),
						container.NewHBox(layout.NewSpacer(), noBtn, yesBtn),
					), w)
				noBtn.OnTapped = func() { trimDlg.Hide() }
				yesBtn.OnTapped = func() { trimDlg.Hide(); runFitBody() }
				trimDlg.Show()
			} else {
				runFitBody()
			}
		}
	})
	fitBtn.Importance = widget.HighImportance

	// Monte Carlo UI elements
	mcNumTrialsEntry := widget.NewEntry()
	mcNumTrialsEntry.SetText("1000")
	mcNumTrialsEntry.SetPlaceHolder("number of trials")
	mcProgressBar := widget.NewProgressBar()
	mcProgressBar.Hide()

	var mcAbortFlag atomic.Bool
	var mcAbortBtn *widget.Button
	mcAbortBtn = widget.NewButton("Abort", func() {
		mcAbortFlag.Store(true)
		mcAbortBtn.Disable()
	})
	mcAbortBtn.Importance = widget.DangerImportance
	mcAbortBtn.Hide()

	var mcBtn *widget.Button
	mcBtn = widget.NewButton("Run Monte Carlo", func() {
		if lastFitResult == nil || lastFitParams == nil {
			dialog.ShowError(fmt.Errorf("no fit result available — run a fit first"), w)
			return
		}
		if len(lastFitCandidates) == 0 {
			dialog.ShowError(fmt.Errorf("no candidate curves available — run a fit first"), w)
			return
		}
		if noiseSigma == 0 {
			dialog.ShowError(fmt.Errorf("no noise sigma available — run Normalize Baseline first"), w)
			return
		}
		numTrials, err := strconv.Atoi(mcNumTrialsEntry.Text)
		if err != nil || numTrials < 1 {
			dialog.ShowError(fmt.Errorf("number of Monte Carlo trials must be a positive integer"), w)
			return
		}
		// Capture all fit state on the main thread before launching the goroutine.
		// This ensures MC always uses the candidates and fit result that were in
		// place when the user clicked Run Monte Carlo, regardless of any concurrent
		// changes (e.g., a new fit started during the MC run).
		mcCandidates := lastFitCandidates
		if mcNarrowSearchCheck.Checked && len(mcCandidates) > 41 {
			center := lastFitBestIdx
			lo := center - 20
			if lo < 0 {
				lo = 0
			}
			hi := center + 20 + 1 // exclusive upper bound
			if hi > len(mcCandidates) {
				hi = len(mcCandidates)
			}
			mcCandidates = mcCandidates[lo:hi]
			logAction(fmt.Sprintf("MC narrow search: using %d candidates (indices %d–%d) around best offset at index %d", len(mcCandidates), lo, hi-1, center))
		}
		mcFitParams := lastFitParams
		// Re-extract target data from the current trim range so that the user can
		// adjust the trim after the fit search and have MC honor it.
		var displayedColIdx int
		for k := range ac.displayedCurves {
			displayedColIdx = k
			break
		}
		col := loadedLightCurveData.Columns[displayedColIdx]
		var mcTargetTimes, mcTargetValues []float64
		for i, val := range col.Values {
			frameNum := loadedLightCurveData.FrameNumbers[i]
			if ac.frameRangeStart > 0 && frameNum < ac.frameRangeStart {
				continue
			}
			if ac.frameRangeEnd > 0 && frameNum > ac.frameRangeEnd {
				continue
			}
			mcTargetTimes = append(mcTargetTimes, loadedLightCurveData.TimeValues[i])
			mcTargetValues = append(mcTargetValues, val)
		}
		if len(mcTargetTimes) < 2 {
			dialog.ShowError(fmt.Errorf("not enough data points in current trim range for Monte Carlo"), w)
			return
		}
		// Resample the best-fit theoretical curve at the current trim target times
		// so that MC noise injection and refitting use only the trimmed range.
		bestPC := lastFitCandidates[lastFitBestIdx]
		trimmedSampledVals := make([]float64, len(mcTargetTimes))
		for i, t := range mcTargetTimes {
			localT := t - lastFitResult.bestShift
			if localT < 0 || localT > bestPC.duration {
				trimmedSampledVals[i] = 1.0
			} else {
				trimmedSampledVals[i] = interpolateAt(bestPC.curve, bestPC.curveTimes, localT)
			}
		}
		mcFitResult := &fitResult{
			curve:        lastFitResult.curve,
			edgeTimes:    lastFitResult.edgeTimes,
			nccCurve:     lastFitResult.nccCurve,
			bestNCC:      lastFitResult.bestNCC,
			bestShift:    lastFitResult.bestShift,
			sampledTimes: mcTargetTimes,
			sampledVals:  trimmedSampledVals,
			bestScale:    lastFitResult.bestScale,
		}
		mcNoiseSigma := noiseSigma
		mcTitle := lastDiffractionTitle
		logAction(fmt.Sprintf("Run Monte Carlo: %d candidate curves from fit search, noise sigma=%.6f, trim range %.0f–%.0f", len(mcCandidates), mcNoiseSigma, ac.frameRangeStart, ac.frameRangeEnd))
		mcProgressBar.SetValue(0)
		mcProgressBar.Show()
		mcBtn.Disable()
		mcAbortFlag.Store(false)
		mcAbortBtn.Show()
		mcAbortBtn.Enable()
		go func() {
			// Yield for two Fyne render frames (~32 ms at 60 fps) before starting trials.
			// Without this, a fast MC run can post fyne.Do(Hide) to the event queue
			// before the Show() calls above have been rendered, making the progress bar
			// and Abort button appear to never show up (rare race condition).
			time.Sleep(32 * time.Millisecond)
			// Pass AR parameters when "Use correlated noise" is checked.
			var mcArPhi []float64
			var mcArSigma2 float64
			if ac.useCorrelatedNoise && len(ac.arPhi) > 0 {
				mcArPhi = ac.arPhi
				mcArSigma2 = ac.arSigma2
				logAction("Monte Carlo: using correlated AR noise")
			}
			result, err := runMonteCarloTrials(mcCandidates, mcFitResult, mcNoiseSigma, numTrials, mcArPhi, mcArSigma2, &mcAbortFlag, func(progress float64) {
				fyne.Do(func() {
					mcProgressBar.SetValue(progress)
				})
			})
			fyne.Do(func() {
				mcProgressBar.Hide()
				mcAbortBtn.Hide()
				mcBtn.Enable()
				if err != nil {
					dialog.ShowError(err, w)
					return
				}
				lastMCResult = result
				mcBtn.Importance = widget.WarningImportance
				mcBtn.Refresh()
				msg := fmt.Sprintf("Monte Carlo results (%d trials):\n\n", result.numTrials)
				for i := 0; i < result.numEdges && i < len(mcFitResult.edgeTimes); i++ {
					absTime := mcFitResult.edgeTimes[i] + mcFitResult.bestShift
					ts := formatSecondsAsTimestamp(absTime)
					msg += fmt.Sprintf("  Edge %d: %s +/- %.4f sec (3 sigma)\n", i+1, ts, 3*result.edgeStds[i])
				}
				if result.numEdges == 2 {
					fitDuration := math.Abs(mcFitResult.edgeTimes[1] - mcFitResult.edgeTimes[0])
					durationStd := math.Sqrt(result.edgeStds[0]*result.edgeStds[0] + result.edgeStds[1]*result.edgeStds[1])
					msg += fmt.Sprintf("\n  Duration: %.4f +/- %.4f sec (3 sigma)\n", fitDuration, 3*durationStd)
				}
				fmt.Print(msg)

				// Log Monte Carlo results
				logAction(fmt.Sprintf("Monte Carlo results (%d trials):", result.numTrials))
				for i := 0; i < result.numEdges; i++ {
					logAction(fmt.Sprintf("  Edge %d: mean=%.4f sec, 3 sigma=%.4f sec", i+1, result.edgeMeans[i], 3*result.edgeStds[i]))
				}
				if result.numEdges == 2 {
					durationMean := math.Abs(result.edgeMeans[1] - result.edgeMeans[0])
					durationStd := math.Sqrt(result.edgeStds[0]*result.edgeStds[0] + result.edgeStds[1]*result.edgeStds[1])
					logAction(fmt.Sprintf("  Duration: mean=%.4f sec, 3 sigma=%.4f sec", durationMean, 3*durationStd))
				}

				// Final report: fit edge times (as timestamps) with MC uncertainty
				logAction("--- Final Report ---")
				logAction(fmt.Sprintf("  NCC=%.4f, path offset=%.3f km", mcFitResult.bestNCC, mcFitParams.PathPerpendicularOffsetKm))
				if mcFitResult.bestScale > 0 {
					logAction(fmt.Sprintf("  Percent drop: %.2f%%", mcFitResult.bestScale*100.0))
				}
				if lastCsvExposureSecs > 0 {
					logAction(fmt.Sprintf("  Camera exposure time: %.6f seconds", lastCsvExposureSecs))
				} else {
					logAction("  Camera exposure time: not set")
				}
				for i, et := range mcFitResult.edgeTimes {
					absTime := et + mcFitResult.bestShift
					ts := formatSecondsAsTimestamp(absTime)
					if i < result.numEdges {
						logAction(fmt.Sprintf("  Edge %d: %s +/- %.4f sec (3 sigma)", i+1, ts, 3*result.edgeStds[i]))
					} else {
						logAction(fmt.Sprintf("  Edge %d: %s", i+1, ts))
					}
				}
				if len(mcFitResult.edgeTimes) == 2 && result.numEdges == 2 {
					fitDuration := math.Abs((mcFitResult.edgeTimes[1] + mcFitResult.bestShift) - (mcFitResult.edgeTimes[0] + mcFitResult.bestShift))
					durationStd := math.Sqrt(result.edgeStds[0]*result.edgeStds[0] + result.edgeStds[1]*result.edgeStds[1])
					logAction(fmt.Sprintf("  Duration: %.4f +/- %.4f sec (3 sigma)", fitDuration, 3*durationStd))
				}
				logAction("--- End Report ---")

				summaryLabel := widget.NewLabel(msg)
				summaryLabel.Wrapping = fyne.TextWrapWord

				var mcContainer *fyne.Container
				if ac.showDiagnostics {
					// Show individual trial edge times (max 100)
					trialsMsg := "Individual trial edge times:\n"
					numCompleted := 0
					if result.numEdges > 0 {
						numCompleted = len(result.edgeAll[0])
					}
					maxDisplay := numCompleted
					if maxDisplay > 100 {
						maxDisplay = 100
					}
					for t := 0; t < maxDisplay; t++ {
						trialsMsg += fmt.Sprintf("  Trial %3d:", t+1)
						for i := 0; i < result.numEdges; i++ {
							trialsMsg += fmt.Sprintf("  Edge %d=%.4f", i+1, result.edgeAll[i][t])
						}
						if result.numEdges == 2 {
							trialsMsg += fmt.Sprintf("  Dur=%.4f", math.Abs(result.edgeAll[1][t]-result.edgeAll[0][t]))
						}
						if t < len(result.pathOffsets) {
							trialsMsg += fmt.Sprintf("  Path=%.3f km", result.pathOffsets[t])
						}
						trialsMsg += "\n"
					}
					if numCompleted > 100 {
						trialsMsg += fmt.Sprintf("  ... (%d more trials not shown)\n", numCompleted-100)
					}
					fmt.Print(trialsMsg)
					trialsLabel := widget.NewLabel(trialsMsg)
					trialsLabel.TextStyle.Monospace = true
					trialsScroll := container.NewScroll(trialsLabel)
					trialsScroll.SetMinSize(fyne.NewSize(750, 300))
					mcContainer = container.NewVBox(summaryLabel, trialsScroll)

					// Show edge and duration histograms
					for i := 0; i < result.numEdges; i++ {
						if len(result.edgeAll[i]) < 2 {
							continue
						}
						histImg, err := createHistogramImage(
							result.edgeAll[i],
							fmt.Sprintf("Edge %d Times", i+1),
							"Time (seconds)",
							mcTitle,
							900, 500,
						)
						if err != nil {
							fmt.Printf("Failed to create Edge %d histogram: %v", i+1, err)
							continue
						}
						histWin := a.NewWindow(fmt.Sprintf("Monte Carlo — Edge %d Histogram", i+1))
						histCanvas := canvas.NewImageFromImage(histImg)
						histCanvas.FillMode = canvas.ImageFillContain
						histWin.SetContent(histCanvas)
						histWin.Resize(fyne.NewSize(950, 550))
						histWin.CenterOnScreen()
						histWin.Show()
					}

					// Show duration histogram if 2 edges
					if result.numEdges == 2 && len(result.edgeAll[0]) >= 2 {
						n := len(result.edgeAll[0])
						durations := make([]float64, n)
						for t := 0; t < n; t++ {
							durations[t] = math.Abs(result.edgeAll[1][t] - result.edgeAll[0][t])
						}
						histImg, err := createHistogramImage(
							durations,
							"Event Duration",
							"Duration (seconds)",
							mcTitle,
							900, 500,
						)
						if err != nil {
							fmt.Printf("Failed to create duration histogram: %v", err)
						} else {
							histWin := a.NewWindow("Monte Carlo — Duration Histogram")
							histCanvas := canvas.NewImageFromImage(histImg)
							histCanvas.FillMode = canvas.ImageFillContain
							histWin.SetContent(histCanvas)
							histWin.Resize(fyne.NewSize(950, 550))
							histWin.CenterOnScreen()
							histWin.Show()
						}
					}
				} else {
					mcSpacer := canvas.NewRectangle(color.Transparent)
					mcSpacer.SetMinSize(fyne.NewSize(750, 0))
					mcContainer = container.NewVBox(mcSpacer, summaryLabel)
				}
				dialog.ShowCustom("Monte Carlo Edge Time Uncertainty", "OK", mcContainer, w)
				// Create a fit overlay plot with ±3σ edge uncertainty lines, using the
				// scale-adjusted theoretical curve (bestScale from the post-fit scale search).
				if len(mcTargetTimes) > 0 && len(mcTargetValues) > 0 {
					mcScale := mcFitResult.bestScale
					if mcScale == 0 {
						mcScale = 1.0
					}
					mcScaledCurve := make([]timeIntensityPoint, len(mcFitResult.curve))
					for i, pt := range mcFitResult.curve {
						mcScaledCurve[i] = timeIntensityPoint{
							time:      pt.time,
							intensity: pt.intensity*mcScale + (1.0 - mcScale),
						}
					}
					mcScaledSampledVals := make([]float64, len(mcFitResult.sampledVals))
					for i, v := range mcFitResult.sampledVals {
						mcScaledSampledVals[i] = v*mcScale + (1.0 - mcScale)
					}
					mcOverlayTitle := mcTitle
					if mcOverlayTitle != "" {
						mcOverlayTitle += fmt.Sprintf("  path offset=%.3f km", mcFitParams.PathPerpendicularOffsetKm)
					}
					if len(mcArPhi) > 0 {
						mcOverlayTitle += "  (correlated noise)"
					} else {
						mcOverlayTitle += "  (uncorrelated noise)"
					}
					mcOverlayImg, err := createOverlayPlotImage(mcScaledCurve, mcFitResult.bestShift, mcFitResult.edgeTimes, mcTargetTimes, mcTargetValues, mcFitResult.sampledTimes, mcScaledSampledVals, mcOverlayTitle, 1200, 500, result.edgeStds)
					if err != nil {
						fmt.Printf("Failed to create MC overlay plot: %v\n", err)
					} else {
						// Save to the results folder
						savePath := filepath.Join(appDir, "fitPlotMC.png")
						if resultsFolder != "" {
							savePath = filepath.Join(resultsFolder, "fitPlotMC.png")
						}
						var buf bytes.Buffer
						if err := png.Encode(&buf, mcOverlayImg); err != nil {
							fmt.Printf("Warning: could not encode fitPlotMC.png: %v\n", err)
						} else if err := os.WriteFile(savePath, buf.Bytes(), 0644); err != nil {
							fmt.Printf("Warning: could not save fitPlotMC.png: %v\n", err)
						}

						mcOverlayWin := a.NewWindow("Fit Result with Monte Carlo Edge Uncertainty (±3σ)")
						mcOverlayCanvas := canvas.NewImageFromImage(mcOverlayImg)
						mcOverlayCanvas.FillMode = canvas.ImageFillContain
						mcOverlayWin.SetContent(mcOverlayCanvas)
						mcOverlayWin.Resize(fyne.NewSize(1250, 550))
						mcOverlayWin.CenterOnScreen()
						mcOverlayWin.Show()
					}

					// Update main plot: overlay theoretical curve, edge lines, and ±3σ lines
					ac.overlayTheoryCurve(mcFitResult, result.edgeStds)
				}
			})
		}()
	})
	mcBtn.Importance = widget.HighImportance
	mcBtn.Disable()

	var nieAbortFlag atomic.Bool
	var nieAbortBtn *widget.Button
	nieAbortBtn = widget.NewButton("Abort NIE", func() {
		nieAbortFlag.Store(true)
		nieAbortBtn.Disable()
	})
	nieAbortBtn.Importance = widget.DangerImportance
	nieAbortBtn.Hide()

	var runNieBtn *widget.Button
	runNieBtn = widget.NewButton("Run NIE analysis", func() {
		if len(rho) >= 2 {
			testARmethod(rho)
		}
		if lastFitResult == nil {
			dialog.ShowError(fmt.Errorf("no fit result available â run a fit first"), w)
			return
		}
		if noiseSigma == 0 {
			dialog.ShowError(fmt.Errorf("no noise sigma available â run Normalize Baseline first"), w)
			return
		}
		if len(lastFitTargetTimes) == 0 {
			dialog.ShowError(fmt.Errorf("no target light curve available â run a fit first"), w)
			return
		}
		mcTrials, err := strconv.Atoi(mcNumTrialsEntry.Text)
		if err != nil || mcTrials < 1 {
			dialog.ShowError(fmt.Errorf("number of trials must be a positive integer"), w)
			return
		}
		numTrials := mcTrials * 10
		nPoints := len(lastFitTargetTimes)

		// Pass AR parameters when "Use correlated noise" is checked.
		var nieArPhi []float64
		var nieArSigma2 float64
		if ac.useCorrelatedNoise && len(ac.arPhi) > 0 {
			nieArPhi = ac.arPhi
			nieArSigma2 = ac.arSigma2
			logAction("NIE: using correlated AR noise")
		}

		// launchNIE starts the goroutine given a known windowWidth, eventDrop, and selection source.
		launchNIE := func(windowWidth int, eventDrop float64, manualSelection bool) {
			logAction(fmt.Sprintf("NIE: starting %d trials, nPoints=%d, windowWidth=%d, noiseSigma=%.6f", numTrials, nPoints, windowWidth, noiseSigma))
			mcProgressBar.SetValue(0)
			mcProgressBar.Show()
			runNieBtn.Disable()
			nieAbortFlag.Store(false)
			nieAbortBtn.Show()
			nieAbortBtn.Enable()
			go func() {
				minMeans, err := runNIETrials(numTrials, nPoints, windowWidth, noiseSigma, nieArPhi, nieArSigma2, &nieAbortFlag, func(progress float64) {
					fyne.Do(func() {
						mcProgressBar.SetValue(progress)
					})
				})
				fyne.Do(func() {
					mcProgressBar.Hide()
					nieAbortBtn.Hide()
					runNieBtn.Enable()
					if err != nil {
						dialog.ShowError(err, w)
						return
					}
					nieNoiseLabel := "uncorrelated noise"
					if len(nieArPhi) > 0 {
						nieNoiseLabel = "correlated noise"
					}
					histImg, nieMean, nieSigma, err := createNIEHistogramImage(minMeans, windowWidth, eventDrop, lastDiffractionTitle, nieNoiseLabel, 800, 500)
					if err != nil {
						dialog.ShowError(fmt.Errorf("failed to create NIE histogram: %v", err), w)
						return
					}
					logAction(fmt.Sprintf("NIE: %d trials completed, min-window-mean distribution: mean=%.6f, sigma=%.6f", len(minMeans), nieMean, nieSigma))
					nieWindowTitle := "Noise Induced Drop study — fit-derived"
					if manualSelection {
						nieWindowTitle = "Noise Induced Drop study — manual selection"
					}
					histWindow := a.NewWindow(nieWindowTitle)
					histCanvas := canvas.NewImageFromImage(histImg)
					histCanvas.FillMode = canvas.ImageFillOriginal
					histWindow.SetContent(container.NewScroll(histCanvas))
					histWindow.Resize(fyne.NewSize(850, 550))
					histWindow.CenterOnScreen()
					histWindow.Show()
					runNieBtn.Importance = widget.WarningImportance
					runNieBtn.Refresh()
				})
			}()
		}

		if nieSinglePointCheck.Checked {
			// Manual selection mode: 1 point -> window=1; 2 points -> window=span count.
			p1 := ac.lightCurvePlot.SelectedPoint1Valid
			p2 := ac.lightCurvePlot.SelectedPoint2Valid
			if p1 && p2 {
				// Two-point mode: the window = number of observed samples in the span,
				// the event drop = mean of those observed values.
				x1 := ac.lightCurvePlot.SelectedPoint1Frame
				x2 := ac.lightCurvePlot.SelectedPoint2Frame
				if x1 > x2 {
					x1, x2 = x2, x1
				}
				// Extract current observed values within the trim range.
				var displayedColIdx int
				for k := range ac.displayedCurves {
					displayedColIdx = k
					break
				}
				col := loadedLightCurveData.Columns[displayedColIdx]
				var dropSum float64
				windowWidth := 0
				for i, val := range col.Values {
					frameNum := loadedLightCurveData.FrameNumbers[i]
					if ac.frameRangeStart > 0 && frameNum < ac.frameRangeStart {
						continue
					}
					if ac.frameRangeEnd > 0 && frameNum > ac.frameRangeEnd {
						continue
					}
					t := loadedLightCurveData.TimeValues[i]
					if t >= x1 && t <= x2 {
						dropSum += val
						windowWidth++
					}
				}
				if windowWidth < 1 {
					dialog.ShowError(fmt.Errorf("no target samples found between the two selected points"), w)
					return
				}
				eventDrop := dropSum / float64(windowWidth)
				logAction(fmt.Sprintf("NIE two-point: x1=%.6f x2=%.6f window=%d eventDrop=%.6f", x1, x2, windowWidth, eventDrop))
				launchNIE(windowWidth, eventDrop, true)
			} else if p1 {
				// Single-point mode: window=1, the drop=selected point value.
				y := ac.lightCurvePlot.SelectedPoint1Value
				logAction(fmt.Sprintf("NIE single-point: using selected point value=%.6f", y))
				launchNIE(1, y, true)
			} else {
				dialog.ShowInformation("Manual NIE Selection",
					"No point is currently selected.\n\nSelect one or two points on the light curve, then click Run NIE analysis again.\n\n"+
						"One point selected: window size = 1, event drop = that point's value.\n"+
						"Two points selected: window = number of samples in the span (inclusive), event drop = mean of those samples.", w)
			}
		} else {
			// Normal mode: compute window width from event edges and event drop from the fit.
			// Event drop = 1 - bestScale, where bestScale is the amplitude scale factor
			// found by the post-fit scale search (scaledTLC = bestTLC*scale + (1-scale)).
			// bestScale==0 (zero value, search not run) maps naturally to eventDrop=1.0 (full drop).
			eventDrop := 1.0 - lastFitResult.bestScale
			windowWidth := 0

			if len(lastFitResult.edgeTimes) >= 2 {
				// Two or more edges: count samples between the first two edges.
				edge1Abs := lastFitResult.edgeTimes[0] + lastFitResult.bestShift
				edge2Abs := lastFitResult.edgeTimes[1] + lastFitResult.bestShift
				if edge1Abs > edge2Abs {
					edge1Abs, edge2Abs = edge2Abs, edge1Abs
				}
				for _, t := range lastFitTargetTimes {
					if t >= edge1Abs && t <= edge2Abs {
						windowWidth++
					}
				}
			} else {
				// Fewer than 2 edges: count sampled theoretical points at or below
				// the half-drop value (midpoint between baseline and full drop),
				// then divide by 2 (truncated) to get the window width.
				halfDropLevel := 1.0 - eventDrop/2.0
				belowCount := 0
				for _, v := range lastFitResult.sampledVals {
					if v <= halfDropLevel {
						belowCount++
					}
				}
				windowWidth = belowCount / 2
				logAction(fmt.Sprintf("NIE: <2 edges, using half-drop level=%.4f, belowCount=%d, windowWidth=%d", halfDropLevel, belowCount, windowWidth))
			}

			if windowWidth < 1 {
				dialog.ShowError(fmt.Errorf("no target samples found for NIE window width — check fit result"), w)
				return
			}
			logAction(fmt.Sprintf("NIE fit-derived: bestScale=%.4f, eventDrop=%.4f", lastFitResult.bestScale, eventDrop))
			launchNIE(windowWidth, eventDrop, false)
		}
	})
	runNieBtn.Importance = widget.HighImportance
	runNieBtn.Disable()

	// buildSodisFill creates a sodisPreFill with an optional occultation override.
	buildSodisFill := func(occultationOverride string, onSave func()) {
		occTitle := lastDiffractionTitle
		if lastFitParams != nil && lastFitParams.Title != "" {
			occTitle = normalizeAsteroidTitle(lastFitParams.Title)
		}
		// Compute observer-corrected t0 using the persisted observer GPS location.
		var computedObserverT0 time.Time
		if lastLoadedOccelmntXml != "" && lastObserverLocationSet {
			_, obsT0, _, t0Err := ObserverT0CorrectionFromOWC(
				lastLoadedOccelmntXml,
				lastObserverLatDeg, lastObserverLonDeg, lastObserverAltMeters,
				0, 0, 0)
			if t0Err == nil {
				computedObserverT0 = obsT0
			}
		}
		// Read "Event Time (UT)" from the details file if present.
		var detailsEventTimeUT string
		if loadedLightCurveData != nil && loadedLightCurveData.SourceFilePath != "" {
			obsDir := filepath.Dir(loadedLightCurveData.SourceFilePath)
			if dirEntries, derr := os.ReadDir(obsDir); derr == nil {
				for _, entry := range dirEntries {
					if !entry.IsDir() && strings.Contains(strings.ToLower(entry.Name()), "detail") {
						if fileData, rerr := os.ReadFile(filepath.Join(obsDir, entry.Name())); rerr == nil {
							// Normalize mixed line endings (CR LF and bare CR) to LF before CSV parsing.
							fileData = bytes.ReplaceAll(fileData, []byte("\r\n"), []byte("\n"))
							fileData = bytes.ReplaceAll(fileData, []byte("\r"), []byte("\n"))
							reader := csv.NewReader(bytes.NewReader(fileData))
							reader.FieldsPerRecord = -1
							reader.TrimLeadingSpace = true
							for {
								record, rerr2 := reader.Read()
								if rerr2 != nil {
									break
								}
								if len(record) >= 2 && strings.TrimSpace(record[0]) == "Event Time (UT)" && strings.TrimSpace(record[1]) != "" {
									detailsEventTimeUT = strings.TrimSpace(record[1])
									break
								}
							}
						}
						break
					}
				}
			}
		}
		showSodisReportDialog(w, &sodisPreFill{
			fitResult:           lastFitResult,
			mcResult:            lastMCResult,
			fitParams:           lastFitParams,
			lcData:              loadedLightCurveData,
			occTitle:            occTitle,
			sitePath:            lastLoadedSitePath,
			occelmntXml:         lastLoadedOccelmntXml,
			noiseSigma:          noiseSigma,
			csvExposureSecs:     lastCsvExposureSecs,
			observerT0:          computedObserverT0,
			detailsEventTimeUT:  detailsEventTimeUT,
			vt:                  ac.vizierTab,
			occultationOverride: occultationOverride,
		}, onSave)
	}

	var fillSodisBtn *widget.Button
	fillSodisBtn = widget.NewButton("Fill SODIS report", func() {
		buildSodisFill("", func() {
			fillSodisBtn.Importance = widget.WarningImportance
			fillSodisBtn.Refresh()
		})
	})
	fillSodisBtn.Importance = widget.HighImportance
	fillSodisBtn.Disable()

	var fillSodisNegBtn *widget.Button
	fillSodisNegBtn = widget.NewButton("Fill SODIS Negative", func() {
		buildSodisFill("NEGATIVE", func() {
			sodisNegativeReportSaved = true
			fillSodisNegBtn.Importance = widget.WarningImportance
			fillSodisNegBtn.Refresh()
		})
	})
	fillSodisNegBtn.Importance = widget.HighImportance

	ac.enablePostFitButtons = func() {
		mcBtn.Enable()
		runNieBtn.Enable()
		fillSodisBtn.Enable()
	}

	ac.resetFitButtons = func() {
		fitBtn.Importance = widget.HighImportance
		fitBtn.Refresh()
		mcBtn.Importance = widget.HighImportance
		mcBtn.Disable()
		runNieBtn.Importance = widget.HighImportance
		runNieBtn.Disable()
		fillSodisBtn.Importance = widget.HighImportance
		fillSodisBtn.Disable()
	}

	tab10Content := container.NewStack(tab10Bg, container.NewPadded(container.NewVBox(
		widget.NewLabel("Fit"),
		widget.NewSeparator(),
		widget.NewLabel("1. Click two points to mark a baseline region (pair)"),
		widget.NewLabel("2. Repeat to add more baseline regions"),
		widget.NewLabel("3. Click on a marked point to remove that pair"),
		widget.NewSeparator(),
		container.NewHBox(calcBaselineMeanBtn),
		widget.NewSeparator(),
		searchRangeCard,
		container.NewHBox(fitBtn, fitAbortBtn, widget.NewButton("Help", showSearchRangeHelp)),
		widget.NewSeparator(),
		widget.NewLabel("Monte Carlo trials"),
		mcNumTrialsEntry,
		container.NewHBox(mcNarrowSearchCheck, nieSinglePointCheck),
		container.NewHBox(mcBtn, mcAbortBtn, runNieBtn, nieAbortBtn, fillSodisBtn, fillSodisNegBtn),
		mcProgressBar,
		widget.NewSeparator(),
		fitStatusLabel,
		fitProgressBar,
	)))
	return container.NewTabItem("Fit", tab10Content)
}
