package main

import (
	"fmt"
	"image/color"

	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/pconstantinou/savitzkygolay"
)

// buildSmoothTab constructs the Smoothing/Normalization tab.
func buildSmoothTab(ac *appContext) *container.TabItem {
	w := ac.window
	tab7Bg := ac.makeTabBg(color.RGBA{R: 200, G: 200, B: 230, A: 255}, color.RGBA{R: 50, G: 50, B: 80, A: 255})

	// Status label for smoothing
	smoothStatusLabel := widget.NewLabel("Click on a point to select the reference curve, then click another point to define window size")

	// Clear smooth button
	clearSmoothButton := widget.NewButton("Clear Smooth", func() {
		ac.smoothedSeries = nil
		// Save Y bounds before rebuild
		savedMinY, savedMaxY := ac.lightCurvePlot.GetYBounds()
		ac.rebuildPlot(func() {
			ac.lightCurvePlot.SetYBounds(savedMinY, savedMaxY)
		})
		smoothStatusLabel.SetText("Smooth curve cleared")
		logAction("Cleared Savitzky-Golay smooth curve")
	})

	// Smooth button
	smoothButton := widget.NewButton("Smooth", func() {
		// Check if we have loaded data
		if noDataLoaded(w) {
			return
		}

		// Check if two points are selected
		if ac.lightCurvePlot.selectedSeries < 0 || ac.lightCurvePlot.selectedIndex < 0 {
			dialog.ShowError(fmt.Errorf("point 1 not selected - click on a point to select it"), w)
			return
		}
		if ac.lightCurvePlot.selectedSeries2 < 0 || ac.lightCurvePlot.selectedIndex2 < 0 {
			dialog.ShowError(fmt.Errorf("point 2 not selected - click on another point to define window size"), w)
			return
		}

		// Use the clicked curve (from the first selected point) as the reference
		series := ac.lightCurvePlot.series[ac.lightCurvePlot.selectedSeries]
		refCurveName := series.Name

		// Find the column in loadedLightCurveData that matches the clicked curve name
		var refColumn *LightCurveColumn
		for i := range loadedLightCurveData.Columns {
			if loadedLightCurveData.Columns[i].Name == refCurveName {
				refColumn = &loadedLightCurveData.Columns[i]
				break
			}
		}
		if refColumn == nil {
			dialog.ShowError(fmt.Errorf("could not find reference column for curve: %s", refCurveName), w)
			return
		}

		// Get the indices of the two selected points to determine window size
		idx1 := series.Points[ac.lightCurvePlot.selectedIndex].Index
		series2 := ac.lightCurvePlot.series[ac.lightCurvePlot.selectedSeries2]
		idx2 := series2.Points[ac.lightCurvePlot.selectedIndex2].Index

		// Calculate window size
		windowSize := idx2 - idx1
		if windowSize < 0 {
			windowSize = -windowSize
		}
		windowSize++ // inclusive

		// Get Y values for the reference column
		ys := refColumn.Values
		numPoints := len(ys)

		// Make window size odd if needed (Savitzky-Golay requires an odd window)
		if windowSize%2 == 0 {
			// Prefer adding 1, but subtract 1 if that would exceed available data
			if windowSize+1 <= numPoints {
				windowSize++
			} else {
				windowSize--
			}
		}

		// Minimum window size check
		if windowSize < 3 {
			windowSize = 3
		}

		logAction(fmt.Sprintf("Savitzky-Golay smoothing: window size = %d, reference curve = %s", windowSize, refColumn.Name))

		if numPoints < windowSize {
			dialog.ShowError(fmt.Errorf("not enough data points (%d) for window size %d", numPoints, windowSize), w)
			return
		}

		// Create X values (just indices for the filter)
		xs := make([]float64, numPoints)
		for i := range xs {
			xs[i] = float64(i)
		}

		// Create Savitzky-Golay filter
		filter, err := savitzkygolay.NewFilterWindow(windowSize)
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to create Savitzky-Golay filter: %v", err), w)
			return
		}

		// Apply the filter
		smoothedYs, err := filter.Process(ys, xs)
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to apply Savitzky-Golay filter: %v", err), w)
			return
		}

		// Create the smoothed series
		var smoothPoints []PlotPoint
		for i, y := range smoothedYs {
			smoothPoints = append(smoothPoints, PlotPoint{
				X:      0, // Will be set in ac.rebuildPlot based on frame numbers or timestamps
				Y:      y,
				Index:  i,
				Series: 0,
			})
		}

		ac.smoothedSeries = &PlotSeries{
			Points: smoothPoints,
			Color:  color.RGBA{R: 255, G: 0, B: 255, A: 255}, // Magenta for a smooth curve
			Name:   "Smooth(" + refColumn.Name + ")",
		}

		// Save Y bounds before rebuild
		savedMinY, savedMaxY := ac.lightCurvePlot.GetYBounds()

		// Rebuild the plot to include the smoothed series
		ac.rebuildPlot(func() {
			// Restore Y bounds
			ac.lightCurvePlot.SetYBounds(savedMinY, savedMaxY)
		})

		// Update status
		statusMsg := fmt.Sprintf("Smoothed %s with window size %d", refColumn.Name, windowSize)
		smoothStatusLabel.SetText(statusMsg)
		logAction(statusMsg)
	})

	// Normalize button - uses the smoothed reference curve to normalize all light curves
	normalizeButton := widget.NewButton("Normalize", func() {
		// Check if we have loaded data
		if noDataLoaded(w) {
			return
		}

		// Check if a smoothed curve exists
		if ac.smoothedSeries == nil {
			dialog.ShowError(fmt.Errorf("choose a reference curve and smoothing window size by clicking two points on the desired reference curve"), w)
			return
		}

		// Check that the smoothed series has the same number of points as the data
		if len(ac.smoothedSeries.Points) != len(loadedLightCurveData.FrameNumbers) {
			dialog.ShowError(fmt.Errorf("smoothed curve length (%d) does not match data length (%d) - please re-smooth after any data changes",
				len(ac.smoothedSeries.Points), len(loadedLightCurveData.FrameNumbers)), w)
			return
		}

		// Calculate the mean of the smoothed reference curve
		var sumSmooth float64
		for _, pt := range ac.smoothedSeries.Points {
			sumSmooth += pt.Y
		}
		meanSmooth := sumSmooth / float64(len(ac.smoothedSeries.Points))

		logAction(fmt.Sprintf("Normalizing light curves using smoothed reference (mean = %.4f)", meanSmooth))

		// Apply normalization to all columns: y_norm[i] = (mean * y[i]) / smooth[i]
		for colIdx := range loadedLightCurveData.Columns {
			for i := range loadedLightCurveData.Columns[colIdx].Values {
				smoothVal := ac.smoothedSeries.Points[i].Y
				if smoothVal != 0 {
					loadedLightCurveData.Columns[colIdx].Values[i] =
						(meanSmooth * loadedLightCurveData.Columns[colIdx].Values[i]) / smoothVal
				}
			}
		}

		// Clear the smoothed series since it's now incorporated into the data
		ac.smoothedSeries = nil

		// Set normalization flag for filename generation
		normalizationApplied = true

		// Save Y bounds before rebuild
		savedMinY, savedMaxY := ac.lightCurvePlot.GetYBounds()

		// Rebuild the plot with normalized data
		ac.rebuildPlot(func() {
			// Restore Y bounds
			ac.lightCurvePlot.SetYBounds(savedMinY, savedMaxY)

			// Clear selected points on the reference curve
			ac.lightCurvePlot.selectedSeries = -1
			ac.lightCurvePlot.selectedIndex = -1
			ac.lightCurvePlot.selectedSeries2 = -1
			ac.lightCurvePlot.selectedIndex2 = -1
			ac.lightCurvePlot.selectedSeriesName = ""
			ac.lightCurvePlot.selectedSeriesName2 = ""
			ac.lightCurvePlot.Refresh()
		})

		// Update status
		statusMsg := fmt.Sprintf("Normalized all light curves (reference mean = %.4f)", meanSmooth)
		smoothStatusLabel.SetText(statusMsg)
		logAction(statusMsg)

		dialog.ShowInformation("Normalization Complete",
			fmt.Sprintf("All light curves have been normalized.\n\nReference mean: %.4f\n\nClick 'Undo' to restore original data.", meanSmooth), w)
	})

	// Undo button - reloads the original CSV file to restore original data
	undoNormalizeButton := widget.NewButton("Undo", func() {
		if noDataLoaded(w) {
			return
		}

		if loadedLightCurveData.SourceFilePath == "" {
			dialog.ShowError(fmt.Errorf("no source file path available"), w)
			return
		}

		sourcePath := loadedLightCurveData.SourceFilePath

		// Save the currently displayed curves before reloading
		savedDisplayedCurves := make(map[int]bool)
		for k, v := range ac.displayedCurves {
			savedDisplayedCurves[k] = v
		}

		// Re-read the original file
		data, err := parseLightCurveCSV(sourcePath)
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to reload CSV: %w", err), w)
			return
		}

		loadedLightCurveData = data
		normalizationApplied = false
		ac.smoothedSeries = nil
		ac.theorySeries = nil
		ac.theorySampledSeries = nil
		ac.lightCurvePlot.SetVerticalLines(nil, false)
		ac.lightCurvePlot.SetSigmaLines(nil, false)
		ac.lightCurvePlot.ShowBaselineLine = false

		// Reset interpolated/negative delta indices
		resetInterpolatedIndices()
		resetNegativeDeltaIndices()

		// Run timing analysis (same as initial load)
		timestampsEmpty := true
		for _, t := range data.TimeValues {
			if t != 0 {
				timestampsEmpty = false
				break
			}
		}
		if !timestampsEmpty && len(data.TimeValues) > 1 {
			timingResult := analyzeTimingErrors(data.TimeValues)
			if timingResult != nil && (len(timingResult.CadenceErrors) > 0 || len(timingResult.DroppedFrameErrors) > 0 || len(timingResult.NegativeDeltaErrors) > 0) {
				if len(timingResult.NegativeDeltaErrors) > 0 {
					timingResult.NegativeDeltaFixed = fixNegativeDeltaTimestamps(data, timingResult.NegativeDeltaErrors, timingResult.AverageTimeStep)
				}
				if len(timingResult.DroppedFrameErrors) > 0 {
					timingResult.InterpolatedCount = interpolateDroppedFrames(data, timingResult.DroppedFrameErrors)
				}
				if len(timingResult.NegativeDeltaErrors) > 0 {
					for _, negErr := range timingResult.NegativeDeltaErrors {
						offset := 0
						for _, dropErr := range timingResult.DroppedFrameErrors {
							if dropErr.Index <= negErr.Index {
								offset += dropErr.DroppedCount
							}
						}
						markNegativeDeltaIndex(negErr.Index + offset)
					}
				}
			}

			if timingResult != nil {
				lastCsvExposureSecs = timingResult.MedianTimeStep
			}
		}

		// Clear displayed curves
		for k := range ac.displayedCurves {
			delete(ac.displayedCurves, k)
		}
		ac.lightCurvePlot.SetSeries(nil)

		// Clear selected points
		ac.lightCurvePlot.selectedSeries = -1
		ac.lightCurvePlot.selectedIndex = -1
		ac.lightCurvePlot.selectedSeries2 = -1
		ac.lightCurvePlot.selectedIndex2 = -1
		ac.lightCurvePlot.selectedPointDataIndex = -1
		ac.lightCurvePlot.selectedPointDataIndex2 = -1
		ac.lightCurvePlot.selectedSeriesName = ""
		ac.lightCurvePlot.selectedSeriesName2 = ""
		ac.lightCurvePlot.SelectedPoint1Valid = false
		ac.lightCurvePlot.SelectedPoint2Valid = false

		// Update frame range
		if len(data.FrameNumbers) > 0 {
			ac.minFrameNum = data.FrameNumbers[0]
			ac.maxFrameNum = data.FrameNumbers[len(data.FrameNumbers)-1]
			ac.frameRangeStart = ac.minFrameNum
			ac.frameRangeEnd = ac.maxFrameNum
			ac.startFrameEntry.SetText(fmt.Sprintf("%.0f", ac.frameRangeStart))
			ac.endFrameEntry.SetText(fmt.Sprintf("%.0f", ac.frameRangeEnd))
		}

		// Restore the previously displayed curves
		for colIdx := range savedDisplayedCurves {
			if colIdx < len(data.Columns) {
				ac.toggleLightCurve(colIdx)
			}
		}

		ac.lightCurvePlot.Refresh()
		smoothStatusLabel.SetText("Original data restored from file")
		logAction(fmt.Sprintf("Undo: Reloaded original data from %s", sourcePath))
	})

	tab7Content := container.NewStack(tab7Bg, container.NewPadded(container.NewVBox(
		widget.NewLabel("Savitzky-Golay Smoothing & Normalization"),
		widget.NewSeparator(),
		widget.NewLabel("Smoothing Instructions:"),
		widget.NewLabel("1. Click on a point on the reference curve to select point 1"),
		widget.NewLabel("2. Click on another point to define window size"),
		widget.NewLabel("3. Click 'Smooth' to apply Savitzky-Golay filter"),
		widget.NewLabel("   (The curve of the first clicked point is used as reference)"),
		widget.NewSeparator(),
		container.NewHBox(smoothButton, clearSmoothButton),
		widget.NewSeparator(),
		widget.NewLabel("Normalization (after smoothing):"),
		widget.NewLabel("4. Click 'Normalize' to apply smoothed reference to all curves"),
		widget.NewLabel("Formula: y_norm[i] = (mean_ref * y[i]) / smooth_ref[i]"),
		widget.NewSeparator(),
		container.NewHBox(normalizeButton, undoNormalizeButton),
		widget.NewSeparator(),
		smoothStatusLabel,
	)))
	return container.NewTabItem("Smooth", tab7Content)
}
