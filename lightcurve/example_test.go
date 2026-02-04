package lightcurve_test

import (
	"GoPyOTE/lightcurve"
	"fmt"
	"log"
	//"github.com/bob-anderson-ok/IOTAdiffraction/lightcurve"
)

// ExtractAndPlotLightCurve demonstrates how to use the lightcurve package to:
// 1. Load a 16-bit diffraction image and extract a light curve
// 2. Load a geometric shadow image and detect edges
// 3. Plot the light curve with edge markers
// 4. Draw the observation path on an 8-bit display image
//
// Parameters:
//   - dxKmPerSec: Shadow velocity X component (km/sec)
//   - dyKmPerSec: Shadow velocity Y component (km/sec)
//   - pathOffsetFromCenterKm: Perpendicular offset from the center (km)
//   - fundamentalPlaneWidthKm: Width of the fundamental plane in km
//   - fundamentalPlaneWidthPts: Width of the fundamental plane in pixels
//   - intensityImagePath: Path to the 16-bit diffraction image (e.g., "occultImage16bit.png")
//   - geometricImagePath: Path to the geometric shadow image (e.g., "geometricShadow.png")
func ExtractAndPlotLightCurve(
	dxKmPerSec float64,
	dyKmPerSec float64,
	pathOffsetFromCenterKm float64,
	fundamentalPlaneWidthKm float64,
	fundamentalPlaneWidthPts int,
	intensityImagePath string,
	geometricImagePath string,
) ([]lightcurve.Point, []float64, error) {

	// Create the observation path from parameters
	path := &lightcurve.ObservationPath{
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

	fmt.Printf("Shadow speed: %.3f km/sec\n", path.ShadowSpeedKmPerSec)
	fmt.Printf("Path angle: %.1f degrees\n", path.PathAngleDegrees)
	fmt.Printf("Path direction: %s\n", path.Direction)
	fmt.Printf("Path start: (%.1f, %.1f)\n", path.StartX, path.StartY)
	fmt.Printf("Path end: (%.1f, %.1f)\n", path.EndX, path.EndY)

	// Compute sample points along the path
	path.ComputeSamplePoints()
	fmt.Printf("Generated %d sample points along the observation path\n", len(path.SamplePoints))

	// Load the 16-bit diffraction image
	// The scale factor of 4000 matches what the main application uses
	intensityMatrix, err := lightcurve.LoadGray16PNG(intensityImagePath, 4000.0)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load 16-bit image %s: %w", intensityImagePath, err)
	}

	// Extract the light curve from the intensity matrix
	lightCurveData := lightcurve.ExtractLightCurve(intensityMatrix, path)
	fmt.Printf("Extracted %d light curve points\n", len(lightCurveData))

	// Load the geometric shadow image and detect edges
	geometricMatrix, err := lightcurve.LoadGray8PNG(geometricImagePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load geometric shadow %s: %w", geometricImagePath, err)
	}

	// Find edges in the geometric shadow
	edges := lightcurve.FindEdgesInGeometricShadow(geometricMatrix, path)
	fmt.Printf("Found %d edges in the geometric shadow\n", len(edges))
	for i, edge := range edges {
		distanceKm := edge * path.FundamentalPlaneWidthKm / float64(path.FundamentalPlaneWidthPts)
		fmt.Printf("  Edge %d at pixel distance %.1f (%.3f km)\n", i+1, edge, distanceKm)
	}

	// Create and save the light curve plot
	err = lightcurve.SaveLightCurvePlot("lightcurve_plot.png", lightCurveData, edges, path, 1200, 500)
	if err != nil {
		log.Printf("Could not save light curve plot: %v\n", err)
	} else {
		fmt.Println("Saved light curve plot to lightcurve_plot.png")
	}

	return lightCurveData, edges, nil
}

// Example shows how to call ExtractAndPlotLightCurve with typical parameters
func Example() {
	// These parameters would typically come from your occultation event parameter file
	lightCurveData, edges, err := ExtractAndPlotLightCurve(
		5.074,                  // dxKmPerSec: Shadow velocity X component
		-0.904,                 // dyKmPerSec: Shadow velocity Y component
		-1.18,                  // pathOffsetFromCenterKm: Perpendicular offset from the center
		40.0,                   // fundamentalPlaneWidthKm: Width of fundamental plane in km
		2000,                   // fundamentalPlaneWidthPts: Width of fundamental plane in pixels
		"occultImage16bit.png", // Path to the 16-bit diffraction image
		"geometricShadow.png",  // Path to geometric shadow image
	)

	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	fmt.Printf("\nExtracted %d light curve points with %d edges\n", len(lightCurveData), len(edges))
}
