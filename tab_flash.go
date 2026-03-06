package main

import (
	"fmt"
	"image/color"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// buildFlashTagsTab constructs the Flash Tags tab.
func buildFlashTagsTab(ac *appContext) *container.TabItem {
	w := ac.window
	tab6Bg := ac.makeTabBg(color.RGBA{R: 220, G: 200, B: 220, A: 255}, color.RGBA{R: 70, G: 50, B: 70, A: 255})

	// Alevel, Blevel display labels (read-only)
	alevelValue := widget.NewLabel("---")
	blevelValue := widget.NewLabel("---")

	// Two flashEdgeNum calculations with stored values
	var savedFlashEdge1, savedFlashEdge2 float64
	var savedPoint1Frame float64 // Frame number of the point used for Edge 1
	flashEdge1Valid := false
	flashEdge2Valid := false
	flashEdge1Value := widget.NewLabel("---")
	flashEdge2Value := widget.NewLabel("---")

	// Helper function to compute flash edge number from the current selection
	computeFlashEdge := func() (float64, bool) {
		// Check if a point is selected
		if ac.lightCurvePlot.selectedSeries < 0 || ac.lightCurvePlot.selectedIndex < 0 {
			dialog.ShowError(fmt.Errorf("no point selected - click on a point first"), w)
			return 0, false
		}

		// Check if we have loaded data
		if loadedLightCurveData == nil {
			dialog.ShowError(fmt.Errorf("no light curve data loaded"), w)
			return 0, false
		}

		// Get the selected series and find the data
		series := ac.lightCurvePlot.series[ac.lightCurvePlot.selectedSeries]
		selectedIdx := ac.lightCurvePlot.selectedIndex
		selectedPointValue := series.Points[selectedIdx].Y

		// Get the frame number of the selected point
		selectedFrameNum := float64(series.Points[selectedIdx].Index)
		if loadedLightCurveData != nil && series.Points[selectedIdx].Index < len(loadedLightCurveData.FrameNumbers) {
			selectedFrameNum = loadedLightCurveData.FrameNumbers[series.Points[selectedIdx].Index]
		}
		logAction(fmt.Sprintf("Flash tag: Computing levels for selected point at frame %.0f, value %.4f", selectedFrameNum, selectedPointValue))

		// Compute Alevel: average of 10 points to the left (before selected point)
		aCount := 0
		aSum := 0.0
		for i := selectedIdx - 1; i >= 0 && aCount < 10; i-- {
			aSum += series.Points[i].Y
			aCount++
		}

		var alevel float64
		alevelValid := false
		if aCount == 0 {
			alevelValue.SetText("N/A")
			logAction("Flash tag: Alevel N/A (no points to the left)")
		} else {
			alevel = aSum / float64(aCount)
			alevelValid = true
			alevelValue.SetText(fmt.Sprintf("%.4f", alevel))
			logAction(fmt.Sprintf("Flash tag: Alevel = %.4f (average of %d points)", alevel, aCount))
		}

		// Compute Blevel: average of 10 points to the right (after the selected point)
		bCount := 0
		bSum := 0.0
		for i := selectedIdx + 1; i < len(series.Points) && bCount < 10; i++ {
			bSum += series.Points[i].Y
			bCount++
		}

		var blevel float64
		blevelValid := false
		if bCount == 0 {
			blevelValue.SetText("N/A")
			logAction("Flash tag: Blevel N/A (no points to the right)")
		} else {
			blevel = bSum / float64(bCount)
			blevelValid = true
			blevelValue.SetText(fmt.Sprintf("%.4f", blevel))
			logAction(fmt.Sprintf("Flash tag: Blevel = %.4f (average of %d points)", blevel, bCount))
		}

		// Compute flashEdgeNum = (Blevel - selected point value) / (Blevel - Alevel) + selected point frame num - 1.0
		if alevelValid && blevelValid {
			// Check for rising edge (Alevel must be less than Blevel)
			if alevel > blevel {
				dialog.ShowError(fmt.Errorf("flash tag edges must always be rising edges (falling edges are subject to slow responses which could cause timing inaccuracies)"), w)
				logAction(fmt.Sprintf("Flash tag: Error - not a rising edge"))
				return 0, false
			}
			denominator := blevel - alevel
			if denominator == 0 {
				logAction("Flash tag: flashEdgeNum N/A (division by zero, Blevel equals Alevel)")
				return 0, false
			}
			flashEdgeNum := (blevel-selectedPointValue)/denominator + selectedFrameNum - 1.0
			logAction(fmt.Sprintf("Flash tag: flashEdgeNum = %.4f", flashEdgeNum))
			return flashEdgeNum, true
		}
		logAction("Flash tag: flashEdgeNum N/A (Alevel or Blevel unavailable)")
		return 0, false
	}

	// Button to compute and save flash edge 1
	computeEdge1Btn := widget.NewButton("Use selected point as Flash 1", func() {
		if val, ok := computeFlashEdge(); ok {
			// The +1 is to maintain the QHY camera model (zero camera delay)
			savedFlashEdge1 = val + 1
			flashEdge1Valid = true
			flashEdge1Value.SetText(fmt.Sprintf("%.4f", savedFlashEdge1))
			// Save the frame number of the point used for Edge 1
			if ac.lightCurvePlot.selectedSeries >= 0 && ac.lightCurvePlot.selectedIndex >= 0 {
				series := ac.lightCurvePlot.series[ac.lightCurvePlot.selectedSeries]
				pointDataIdx := series.Points[ac.lightCurvePlot.selectedIndex].Index
				if loadedLightCurveData != nil && pointDataIdx < len(loadedLightCurveData.FrameNumbers) {
					savedPoint1Frame = loadedLightCurveData.FrameNumbers[pointDataIdx]
				}
			}
			logAction(fmt.Sprintf("Flash tag: Saved Edge 1 = %.4f, Point1 Frame = %.0f", savedFlashEdge1, savedPoint1Frame))
		} else {
			flashEdge1Value.SetText("N/A")
			flashEdge1Valid = false
		}
	})

	// Button to compute and save flash edge 2
	computeEdge2Btn := widget.NewButton("Use selected point as Flash 2", func() {
		if val, ok := computeFlashEdge(); ok {
			// The +1 is to maintain the QHY camera model (zero camera delay)
			savedFlashEdge2 = val + 1
			flashEdge2Valid = true
			flashEdge2Value.SetText(fmt.Sprintf("%.4f", savedFlashEdge2))
			logAction(fmt.Sprintf("Flash tag: Saved Edge 2 = %.4f", savedFlashEdge2))
		} else {
			flashEdge2Value.SetText("N/A")
			flashEdge2Valid = false
		}
	})

	// Timestamp entry boxes with parsed values
	var timestamp1Seconds, timestamp2Seconds float64
	var timestamp1Valid, timestamp2Valid bool

	timestamp1Entry := NewFocusLossEntry()
	timestamp1Entry.SetPlaceHolder("hh:mm:ss.ssss")
	timestamp1Entry.OnSubmitted = func(text string) {
		if text == "" {
			timestamp1Valid = false
			return
		}
		if val, ok := parseTimestampInput(text); ok {
			timestamp1Seconds = val
			timestamp1Valid = true
			logAction(fmt.Sprintf("Flash tag: Timestamp 1 = %s (%.4f seconds)", text, val))
		} else {
			timestamp1Valid = false
			dialog.ShowError(fmt.Errorf("invalid timestamp format: %s", text), w)
		}
	}

	timestamp2Entry := NewFocusLossEntry()
	timestamp2Entry.SetPlaceHolder("hh:mm:ss.ssss")
	timestamp2Entry.OnSubmitted = func(text string) {
		if text == "" {
			timestamp2Valid = false
			return
		}
		if val, ok := parseTimestampInput(text); ok {
			timestamp2Seconds = val
			timestamp2Valid = true
			logAction(fmt.Sprintf("Flash tag: Timestamp 2 = %s (%.4f seconds)", text, val))
		} else {
			timestamp2Valid = false
			dialog.ShowError(fmt.Errorf("invalid timestamp format: %s", text), w)
		}
	}

	// Wrap timestamp entries in containers
	timestamp1Container := container.New(layout.NewGridWrapLayout(fyne.NewSize(187.5, 36)), timestamp1Entry)
	timestamp2Container := container.New(layout.NewGridWrapLayout(fyne.NewSize(187.5, 36)), timestamp2Entry)

	// Camera exposure time entry
	var cameraExposureTime float64
	exposureTimeEntry := NewFocusLossEntry()
	exposureTimeEntry.SetPlaceHolder("seconds")
	exposureTimeEntry.OnSubmitted = func(text string) {
		if text == "" {
			return
		}
		val, err := strconv.ParseFloat(text, 64)
		if err != nil {
			dialog.ShowError(fmt.Errorf("invalid exposure time: %s", text), w)
			return
		}
		cameraExposureTime = val
		logAction(fmt.Sprintf("Flash tag: Camera exposure time set to %.4f seconds", cameraExposureTime))
	}
	exposureTimeContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(100, 36)), exposureTimeEntry)

	// Time per frame calculation
	timePerFrameValue := widget.NewLabel("---")
	var timePerFrame float64
	calcTimePerFrameBtn := widget.NewButton("Calc time/frame", func() {
		if !timestamp1Valid || !timestamp2Valid {
			dialog.ShowError(fmt.Errorf("both timestamps must be entered"), w)
			timePerFrameValue.SetText("N/A")
			return
		}
		if !flashEdge1Valid || !flashEdge2Valid {
			dialog.ShowError(fmt.Errorf("both edge values must be set"), w)
			timePerFrameValue.SetText("N/A")
			return
		}
		edgeDiff := savedFlashEdge2 - savedFlashEdge1
		if edgeDiff == 0 {
			dialog.ShowError(fmt.Errorf("edge 2 and edge 1 are equal (division by zero)"), w)
			timePerFrameValue.SetText("N/A")
			return
		}
		timePerFrame = (timestamp2Seconds - timestamp1Seconds) / edgeDiff
		timePerFrameValue.SetText(fmt.Sprintf("%.6f", timePerFrame))
		logAction(fmt.Sprintf("Flash tag: timePerFrame = (%.4f - %.4f) / (%.4f - %.4f) = %.6f seconds",
			timestamp2Seconds, timestamp1Seconds, savedFlashEdge2, savedFlashEdge1, timePerFrame))
	})

	// Tzero calculation: Tzero = timestamp1 - (flash1Frame - minFrame) * time per frame
	tzeroValue := widget.NewLabel("---")
	var tzero float64
	calcTzeroBtn := widget.NewButton("Calc Tzero", func() {
		if !timestamp1Valid {
			dialog.ShowError(fmt.Errorf("timestamp 1 must be entered"), w)
			tzeroValue.SetText("N/A")
			return
		}
		if timePerFrame == 0 {
			dialog.ShowError(fmt.Errorf("time per frame must be calculated first"), w)
			tzeroValue.SetText("N/A")
			return
		}
		if !flashEdge1Valid {
			dialog.ShowError(fmt.Errorf("flash 1 must be set first"), w)
			tzeroValue.SetText("N/A")
			return
		}
		tzero = timestamp1Seconds - (savedFlashEdge1-ac.minFrameNum)*timePerFrame
		tzeroValue.SetText(formatSecondsAsTimestamp(tzero))
		logAction(fmt.Sprintf("Flash tag: Tzero = %.4f - (%.4f - %.0f) * %.6f = %.4f (%s)",
			timestamp1Seconds, savedFlashEdge1, ac.minFrameNum, timePerFrame, tzero, formatSecondsAsTimestamp(tzero)))

		// Update all light curve timestamps: timestamp = Tzero + (frameNumber - ac.minFrameNum) * timePerFrame
		if loadedLightCurveData != nil {
			for i, frameNum := range loadedLightCurveData.FrameNumbers {
				loadedLightCurveData.TimeValues[i] = tzero + (frameNum-ac.minFrameNum)*timePerFrame
			}
			logAction(fmt.Sprintf("Flash tag: Updated %d light curve timestamps", len(loadedLightCurveData.TimeValues)))
			dialog.ShowInformation("Timestamps updated/inserted",
				fmt.Sprintf("Updated %d light curve timestamps", len(loadedLightCurveData.TimeValues)), w)
		}
	})

	// Frame timestamp calculation: frameTime = tzero + (frameNum - ac.minFrameNum) * timePerFrame
	frameNumEntry := NewFocusLossEntry()
	frameNumEntry.SetPlaceHolder("frame #")
	frameNumContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(80, 36)), frameNumEntry)
	frameTimeValue := widget.NewLabel("---")
	calcFrameTimeBtn := widget.NewButton("Calc frame time", func() {
		if tzero == 0 {
			dialog.ShowError(fmt.Errorf("tzero must be calculated first"), w)
			frameTimeValue.SetText("N/A")
			return
		}
		if timePerFrame == 0 {
			dialog.ShowError(fmt.Errorf("time per frame must be calculated first"), w)
			frameTimeValue.SetText("N/A")
			return
		}
		frameNumText := frameNumEntry.Text
		if frameNumText == "" {
			dialog.ShowError(fmt.Errorf("frame number must be entered"), w)
			frameTimeValue.SetText("N/A")
			return
		}
		frameNum, err := strconv.ParseFloat(frameNumText, 64)
		if err != nil {
			dialog.ShowError(fmt.Errorf("invalid frame number: %s", frameNumText), w)
			frameTimeValue.SetText("N/A")
			return
		}
		frameTime := tzero + (frameNum-ac.minFrameNum)*timePerFrame
		frameTimeValue.SetText(formatSecondsAsTimestamp(frameTime))
		logAction(fmt.Sprintf("Flash tag: Frame %g time = %.4f + (%g - %.0f) * %.6f = %.4f (%s)",
			frameNum, tzero, frameNum, ac.minFrameNum, timePerFrame, frameTime, formatSecondsAsTimestamp(frameTime)))
	})

	// Suppress unused variable warning
	_ = cameraExposureTime

	tab6Content := container.NewStack(tab6Bg, container.NewPadded(container.NewVBox(
		widget.NewLabel("Flash tags"),
		container.NewHBox(widget.NewLabel("Alevel:"), alevelValue),
		container.NewHBox(widget.NewLabel("Blevel:"), blevelValue),
		container.NewHBox(computeEdge1Btn, widget.NewLabel("Flash 1 frame"), flashEdge1Value),
		container.NewHBox(computeEdge2Btn, widget.NewLabel("Flash 2 frame"), flashEdge2Value),
		container.NewHBox(widget.NewLabel("Flash 1 timestamp"), timestamp1Container),
		container.NewHBox(widget.NewLabel("Flash 2 timestamp"), timestamp2Container),
		container.NewHBox(widget.NewLabel("Exposure time:"), exposureTimeContainer),
		container.NewHBox(calcTimePerFrameBtn, widget.NewLabel("Time/frame:"), timePerFrameValue),
		container.NewHBox(calcTzeroBtn, widget.NewLabel("Tzero:"), tzeroValue),
		container.NewHBox(calcFrameTimeBtn, widget.NewLabel("Frame:"), frameNumContainer, widget.NewLabel("Time:"), frameTimeValue),
	)))
	return container.NewTabItem("Flash tags", tab6Content)
}
