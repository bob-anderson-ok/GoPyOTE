package lightcurve_test

import (
	"GoPyOTE/lightcurve"
	"fmt"
	"log"

	"fyne.io/fyne/v2"
)

// Example shows how to call ExtractAndPlotLightCurve with typical parameters
func Example(app fyne.App) {
	// These parameters would typically come from your occultation event parameter file
	lightCurveData, edges, err := lightcurve.ExtractAndPlotLightCurve(
		app,                        // Fyne app for displaying windows (pass nil to skip display)
		5.074,                      // dxKmPerSec: Shadow velocity X component
		-0.904,                     // dyKmPerSec: Shadow velocity Y component
		-1.18,                      // pathOffsetFromCenterKm: Perpendicular offset from the center
		40.0,                       // fundamentalPlaneWidthKm: Width of fundamental plane in km
		2000,                       // fundamentalPlaneWidthPts: Width of fundamental plane in pixels
		"occultImage16bit.png",     // Path to the 16-bit diffraction image
		"geometricShadow.png",      // Path to geometric shadow image
		"diffractionImage8bit.png", // Path to 8-bit display image for path overlay
	)

	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	fmt.Printf("\nExtracted %d light curve points with %d edges\n", len(lightCurveData), len(edges))
}
