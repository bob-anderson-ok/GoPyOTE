package main

import (
	"fmt"
	"image/color"

	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// buildBlockIntTab constructs the Block Integration tab.
func buildBlockIntTab(ac *appContext) *container.TabItem {
	w := ac.window
	tab5Bg := ac.makeTabBg(color.RGBA{R: 200, G: 220, B: 200, A: 255}, color.RGBA{R: 50, G: 70, B: 50, A: 255})

	// Status label for block integration
	blockIntStatusLabel := widget.NewLabel("Select two points on the plot to define a block size")

	// Block integrate button
	blockIntegrateButton := widget.NewButton("Block Integrate", func() {
		// Check if we have loaded data
		if loadedLightCurveData == nil {
			dialog.ShowError(fmt.Errorf("no light curve data loaded"), w)
			return
		}

		// Check if two points are selected
		if ac.lightCurvePlot.selectedSeries < 0 || ac.lightCurvePlot.selectedIndex < 0 {
			dialog.ShowError(fmt.Errorf("point 1 not selected - click on a point to select it"), w)
			return
		}
		if ac.lightCurvePlot.selectedSeries2 < 0 || ac.lightCurvePlot.selectedIndex2 < 0 {
			dialog.ShowError(fmt.Errorf("point 2 not selected - click on another point in the same light curve"), w)
			return
		}

		// Check if both points are on the same series
		if ac.lightCurvePlot.selectedSeries != ac.lightCurvePlot.selectedSeries2 {
			dialog.ShowError(fmt.Errorf("both points must be on the same series to define a block"), w)
			return
		}

		// Get the indices of the two selected points (in the original data)
		series := ac.lightCurvePlot.series[ac.lightCurvePlot.selectedSeries]
		idx1 := series.Points[ac.lightCurvePlot.selectedIndex].Index
		idx2 := series.Points[ac.lightCurvePlot.selectedIndex2].Index

		// Ensure idx1 < idx2
		if idx1 > idx2 {
			idx1, idx2 = idx2, idx1
		}

		// Calculate block size (number of points inclusive)
		blockSize := idx2 - idx1 + 1
		if blockSize < 2 {
			dialog.ShowError(fmt.Errorf("block size must be at least 2 points (current: %d)", blockSize), w)
			return
		}

		logAction(fmt.Sprintf("Block Integration: block size = %d points (from index %d to %d)", blockSize, idx1, idx2))

		// Apply block integration to all columns in the loaded data
		numPoints := len(loadedLightCurveData.FrameNumbers)

		if numPoints == 0 {
			dialog.ShowError(fmt.Errorf("no data points in loaded file"), w)
			return
		}

		// Block integration starts from the first selected point (idx1)
		// Calculate complete blocks going left and right from idx1
		pointsBefore := idx1               // points before idx1 (indices 0 to idx1-1)
		pointsFromIdx1 := numPoints - idx1 // points from idx1 to end (indices idx1 to numPoints-1)

		blocksBefore := pointsBefore / blockSize     // complete blocks to the left
		blocksFromIdx1 := pointsFromIdx1 / blockSize // complete blocks from idx1 onward

		numBlocks := blocksBefore + blocksFromIdx1

		if numBlocks == 0 {
			dialog.ShowError(fmt.Errorf("not enough points for even one complete block of size %d", blockSize), w)
			return
		}

		// Calculate where the first complete block starts
		// Blocks to the left: the leftmost complete block starts at idx1 - (blocksBefore * blockSize)
		firstBlockStart := idx1 - (blocksBefore * blockSize)

		logAction(fmt.Sprintf("Block Integration: idx1=%d, blocksBefore=%d, blocksFromIdx1=%d, total=%d, firstBlockStart=%d",
			idx1, blocksBefore, blocksFromIdx1, numBlocks, firstBlockStart))

		// Create new arrays for block-integrated data
		newFrameNumbers := make([]float64, numBlocks)
		newTimeValues := make([]float64, numBlocks)
		newColumns := make([]LightCurveColumn, len(loadedLightCurveData.Columns))

		for i := range newColumns {
			newColumns[i].Name = loadedLightCurveData.Columns[i].Name
			newColumns[i].Values = make([]float64, numBlocks)
		}

		// Process each block starting from the firstBlockStart
		for blockIdx := 0; blockIdx < numBlocks; blockIdx++ {
			startIdx := firstBlockStart + (blockIdx * blockSize)
			endIdx := startIdx + blockSize // exclusive

			// Use the frame number and time of the first point in the block
			newFrameNumbers[blockIdx] = loadedLightCurveData.FrameNumbers[startIdx]
			newTimeValues[blockIdx] = loadedLightCurveData.TimeValues[startIdx]

			// Average each column's values in this block
			for colIdx := range loadedLightCurveData.Columns {
				sum := 0.0
				for i := startIdx; i < endIdx; i++ {
					sum += loadedLightCurveData.Columns[colIdx].Values[i]
				}
				newColumns[colIdx].Values[blockIdx] = sum / float64(blockSize)
			}
		}

		// Update the loaded data with block-integrated values
		loadedLightCurveData.FrameNumbers = newFrameNumbers
		loadedLightCurveData.TimeValues = newTimeValues
		loadedLightCurveData.Columns = newColumns

		// Clear smooth curve since indices are now invalid
		ac.smoothedSeries = nil

		// Update frame range limits
		ac.minFrameNum = newFrameNumbers[0]
		ac.maxFrameNum = newFrameNumbers[len(newFrameNumbers)-1]
		ac.frameRangeStart = ac.minFrameNum
		ac.frameRangeEnd = ac.maxFrameNum
		ac.startFrameEntry.SetText(fmt.Sprintf("%.0f", ac.frameRangeStart))
		ac.endFrameEntry.SetText(fmt.Sprintf("%.0f", ac.frameRangeEnd))

		// Clear selections since indices are now invalid
		ac.lightCurvePlot.selectedSeries = -1
		ac.lightCurvePlot.selectedIndex = -1
		ac.lightCurvePlot.selectedSeries2 = -1
		ac.lightCurvePlot.selectedIndex2 = -1
		ac.lightCurvePlot.selectedPointDataIndex = -1
		ac.lightCurvePlot.selectedPointDataIndex2 = -1
		ac.lightCurvePlot.selectedSeriesName = ""
		ac.lightCurvePlot.selectedSeriesName2 = ""

		// Save Y bounds before rebuilding (preserve user scaling)
		savedMinY, savedMaxY := ac.lightCurvePlot.GetYBounds()

		// Rebuild the plot with the new data
		ac.rebuildPlot(func() {
			// Restore Y bounds to preserve user scaling
			ac.lightCurvePlot.SetYBounds(savedMinY, savedMaxY)
		})

		// Update status
		statusMsg := fmt.Sprintf("Block integrated: %d points → %d blocks (block size: %d)", numPoints, numBlocks, blockSize)
		blockIntStatusLabel.SetText(statusMsg)
		logAction(statusMsg)

		dialog.ShowInformation("Block Integration Complete",
			fmt.Sprintf("Original: %d points\nBlock size: %d\nResult: %d averaged blocks\n\nClick 'Undo' to restore original data.", numPoints, blockSize, numBlocks), w)
	})

	// Undo button - reloads the original CSV file to restore original data
	undoBlockIntButton := widget.NewButton("Undo", func() {
		if loadedLightCurveData == nil {
			dialog.ShowError(fmt.Errorf("no light curve data loaded"), w)
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
		blockIntStatusLabel.SetText("Original data restored from file")
		logAction(fmt.Sprintf("Undo: Reloaded original data from %s", sourcePath))
	})

	tab5Content := container.NewStack(tab5Bg, container.NewPadded(container.NewVBox(
		widget.NewLabel("Block Integration"),
		widget.NewSeparator(),
		widget.NewLabel("Instructions:"),
		widget.NewLabel("1. Click on the first point of a 'block'"),
		widget.NewLabel("2. Click on the last point in that 'block'"),
		widget.NewLabel("3. Click the Block Integrate button"),
		widget.NewSeparator(),
		container.NewHBox(blockIntegrateButton, undoBlockIntButton),
		widget.NewSeparator(),
		blockIntStatusLabel,
	)))
	return container.NewTabItem("BlockInt", tab5Content)
}
