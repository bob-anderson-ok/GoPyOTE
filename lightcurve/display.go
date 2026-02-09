package lightcurve

import (
	"fmt"
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

// verboseExtraction controls whether diagnostic messages are printed during path extraction.
// Set to true to re-enable console output for debugging.
var verboseExtraction = false

// ExtractAndPlotLightCurve demonstrates how to use the lightcurve package to:
// 1. Load a 16-bit diffraction image and extract a light curve
// 2. Load a geometric shadow image and detect edges
// 3. Plot the light curve with edge markers
// 4. Draw the observation path on an 8-bit display image
//
// Parameters:
//   - app: Fyne application instance for creating windows (pass nil to skip window display)
//   - dxKmPerSec: Shadow velocity X component (km/sec)
//   - dyKmPerSec: Shadow velocity Y component (km/sec)
//   - pathOffsetFromCenterKm: Perpendicular offset from the center (km)
//   - fundamentalPlaneWidthKm: Width of the fundamental plane in km
//   - fundamentalPlaneWidthPts: Width of the fundamental plane in pixels
//   - intensityImagePath: Path to the 16-bit diffraction image (e.g., "targetImage16bit.png")
//   - geometricImagePath: Path to the geometric shadow image (e.g., "geometricShadow.png")
//   - displayImagePath: Path to the 8-bit display image for observation path overlay (e.g., "diffractionImage8bit.png")
func ExtractAndPlotLightCurve(
	app fyne.App,
	dxKmPerSec float64,
	dyKmPerSec float64,
	pathOffsetFromCenterKm float64,
	fundamentalPlaneWidthKm float64,
	fundamentalPlaneWidthPts int,
	intensityImagePath string,
	geometricImagePath string,
	displayImagePath string,
) ([]Point, []float64, error) {

	// Create the observation path from parameters
	path := &ObservationPath{
		DxKmPerSec:               dxKmPerSec,
		DyKmPerSec:               dyKmPerSec,
		PathOffsetFromCenterKm:   pathOffsetFromCenterKm,
		FundamentalPlaneWidthKm:  fundamentalPlaneWidthKm,
		FundamentalPlaneWidthPts: fundamentalPlaneWidthPts,
	}

	// Compute the path start/end points from velocity and offset
	err := path.ComputePathFromVelocity()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to compute path: %w", err)
	}

	if verboseExtraction {
		fmt.Printf("Shadow speed: %.3f km/sec\n", path.ShadowSpeedKmPerSec)
		fmt.Printf("Path angle: %.1f degrees\n", path.PathAngleDegrees)
		fmt.Printf("Path direction: %s\n", path.Direction)
		fmt.Printf("Path start: (%.1f, %.1f)\n", path.StartX, path.StartY)
		fmt.Printf("Path end: (%.1f, %.1f)\n", path.EndX, path.EndY)
	}

	// Compute sample points along the path
	path.ComputeSamplePoints()
	if verboseExtraction {
		fmt.Printf("Generated %d sample points along the observation path\n", len(path.SamplePoints))
	}

	// Load the 16-bit diffraction image
	// The scale factor of 4000 matches what the main application uses
	intensityMatrix, err := LoadGray16PNG(intensityImagePath, 4000.0)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load 16-bit image %s: %w", intensityImagePath, err)
	}

	// Extract the light curve from the intensity matrix
	lightCurveData := ExtractLightCurve(intensityMatrix, path)
	if verboseExtraction {
		fmt.Printf("Extracted %d light curve points\n", len(lightCurveData))
	}

	// Load the geometric shadow image and detect edges
	geometricMatrix, err := LoadGray8PNG(geometricImagePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load geometric shadow %s: %w", geometricImagePath, err)
	}

	// Find edges in the geometric shadow
	edges := FindEdgesInGeometricShadow(geometricMatrix, path)
	if verboseExtraction {
		fmt.Printf("Found %d edges in the geometric shadow\n", len(edges))
		for i, edge := range edges {
			distanceKm := edge * path.FundamentalPlaneWidthKm / float64(path.FundamentalPlaneWidthPts)
			fmt.Printf("  Edge %d at pixel distance %.1f (%.3f km)\n", i+1, edge, distanceKm)
		}
	}

	// Create the light curve plot image
	lightCurvePlotImg, err := PlotLightCurve(lightCurveData, edges, path, 1200, 500)
	if err != nil {
		log.Printf("Could not create light curve plot: %v\n", err)
	}

	// Load the 8-bit display image and draw the observation path
	var annotatedImg *canvas.Image
	if displayImagePath != "" {
		displayImg, err := LoadImageFromFile(displayImagePath)
		if err != nil {
			log.Printf("Could not load display image %s: %v\n", displayImagePath, err)
		} else {
			imgWithPath, err := DrawObservationLineOnImage(displayImg, path)
			if err != nil {
				log.Printf("Could not draw observation path: %v\n", err)
			} else {
				annotatedImg = canvas.NewImageFromImage(imgWithPath)
				annotatedImg.FillMode = canvas.ImageFillContain
			}
		}
	}

	// Display images in Fyne windows if an app is provided
	if app != nil {
		// Display the light curve plot in a new window
		if lightCurvePlotImg != nil {
			plotWindow := app.NewWindow("Light Curve Plot")
			plotCanvas := canvas.NewImageFromImage(lightCurvePlotImg)
			plotCanvas.FillMode = canvas.ImageFillOriginal
			plotWindow.SetContent(container.NewScroll(plotCanvas))
			plotWindow.Resize(fyne.NewSize(1250, 550))
			plotWindow.Show()
		}

		// Display the annotated diffraction image in a new window
		if annotatedImg != nil {
			pathWindow := app.NewWindow("Observation Path on Diffraction Image")
			pathWindow.SetContent(container.NewScroll(annotatedImg))
			pathWindow.Resize(fyne.NewSize(600, 600))
			pathWindow.Show()
		}
	}

	return lightCurveData, edges, nil
}
