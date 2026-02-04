// Example program demonstrating how to use the lightcurve package to:
// 1. Load a 16-bit diffraction image and extract a light curve
// 2. Load a geometric shadow image and detect edges
// 3. Plot the light curve with edge markers
// 4. Draw the observation path on an 8-bit display image
//
// Usage:
//
//	go run main.go
//
// This example assumes the following files exist in the current directory:
//   - occultImage16bit.png (16-bit diffraction image)
//   - geometricShadow.png (geometric shadow image)
//   - diffractionImage8bit.png (8-bit display image)
//
// If these files don't exist, the program will generate synthetic test data.
package main

import (
	"GoPyOTE/lightcurve"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

func main() {
	fmt.Println("Light Curve Extraction Example")
	fmt.Println("==============================")

	// Get the directory containing the images (parent of cmd/example)
	workDir, _ := os.Getwd()
	//imageDir := filepath.Join(workDir, "..", "..")
	imageDir := workDir

	// Define the observation path using velocity components and offset
	// These parameters would typically come from your occultation event parameter file:
	//   - dx_km_per_sec: Shadow velocity X component
	//   - dy_km_per_sec: Shadow velocity Y component
	//   - path_offset_from_center_km: Perpendicular offset from the center
	path := &lightcurve.ObservationPath{
		// Shadow velocity components (from the parameter file)
		DxKmPerSec:             5.074,  // Shadow velocity X component (km/sec)
		DyKmPerSec:             -0.904, // Shadow velocity Y component (km/sec)
		PathOffsetFromCenterKm: -1.18,  // Perpendicular offset from the center (km)

		// Fundamental plane dimensions
		FundamentalPlaneWidthKm:  40.0, // Width of the fundamental plane in km
		FundamentalPlaneWidthPts: 2000, // Width of the fundamental plane in pixels
	}

	// Compute the path start/end points from velocity and offset
	err := path.ComputePathFromVelocity()
	if err != nil {
		log.Fatalf("Failed to compute path: %v", err)
	}

	fmt.Printf("\nPath computed from velocity components:")
	fmt.Printf("\n  Shadow speed: %.3f km/sec", path.ShadowSpeedKmPerSec)
	fmt.Printf("\n  Path angle: %.1f degrees", path.PathAngleDegrees)
	fmt.Printf("\n  Path direction: %s", path.Direction)
	fmt.Printf("\n  Path start: (%.1f, %.1f)", path.StartX, path.StartY)
	fmt.Printf("\n  Path end: (%.1f, %.1f)\n", path.EndX, path.EndY)

	// Compute sample points along the path
	path.ComputeSamplePoints()
	fmt.Printf("\nGenerated %d sample points along the observation path\n", len(path.SamplePoints))
	fmt.Printf("Path length: %.1f pixels\n", path.SamplePoints[len(path.SamplePoints)-1].DistanceFromStart)

	// Try to load the 16-bit diffraction image
	intensityFile := filepath.Join(imageDir, "occultImage16bit.png")
	intensityMatrix, err := lightcurve.LoadGray16PNG(intensityFile, 4000.0)
	if err != nil {
		fmt.Printf("\nNote: Could not load %s: %v\n", intensityFile, err)
		fmt.Println("Using synthetic test data instead.")
		intensityMatrix = createTestIntensityMatrix(1000)
	} else {
		fmt.Printf("\nLoaded intensity matrix: %dx%d pixels\n", len(intensityMatrix), len(intensityMatrix[0]))
	}

	// Extract the light curve from the intensity matrix
	lightCurveData := lightcurve.ExtractLightCurve(intensityMatrix, path)
	fmt.Printf("Extracted %d light curve points\n", len(lightCurveData))

	// Print the first few and last few points
	fmt.Println("\nSample light curve data:")
	fmt.Println("  First 3 points:")
	for i := 0; i < 3 && i < len(lightCurveData); i++ {
		fmt.Printf("    Distance: %8.3f km, Intensity: %.4f\n",
			lightCurveData[i].Distance, lightCurveData[i].Intensity)
	}
	fmt.Println("  Last 3 points:")
	for i := len(lightCurveData) - 3; i < len(lightCurveData) && i >= 0; i++ {
		fmt.Printf("    Distance: %8.3f km, Intensity: %.4f\n",
			lightCurveData[i].Distance, lightCurveData[i].Intensity)
	}

	// Try to load the geometric shadow image and detect edges
	geometricFile := filepath.Join(imageDir, "geometricShadow.png")
	geometricMatrix, err := lightcurve.LoadGray8PNG(geometricFile)
	if err != nil {
		fmt.Printf("\nNote: Could not load %s: %v\n", geometricFile, err)
		fmt.Println("Using synthetic test data instead.")
		geometricMatrix = createTestGeometricMatrix(1000)
	} else {
		fmt.Printf("\nLoaded geometric shadow: %dx%d pixels\n", len(geometricMatrix), len(geometricMatrix[0]))
	}

	// Find edges in the geometric shadow
	edges := lightcurve.FindEdgesInGeometricShadow(geometricMatrix, path)
	fmt.Printf("\nFound %d edges in the geometric shadow:\n", len(edges))
	distancePerPoint := path.FundamentalPlaneWidthKm / float64(path.FundamentalPlaneWidthPts)
	for i, edge := range edges {
		distanceKm := edge * distancePerPoint
		fmt.Printf("  Edge %d: pixel distance = %.1f, distance = %.3f km\n", i+1, edge, distanceKm)
	}

	// Create and save the light curve plot
	outputPlot := "lightcurve_plot.png"
	err = lightcurve.SaveLightCurvePlot(outputPlot, lightCurveData, edges, path, 1200, 500)
	if err != nil {
		log.Printf("Could not save light curve plot: %v\n", err)
	} else {
		fmt.Printf("\nSaved light curve plot to %s\n", outputPlot)
	}

	// Try to load the 8-bit display image and draw the observation line
	displayFile := filepath.Join(imageDir, "diffractionImage8bit.png")
	displayImage, err := lightcurve.LoadImageFromFile(displayFile)
	if err != nil {
		fmt.Printf("\nNote: Could not load %s: %v\n", displayFile, err)
		fmt.Println("Skipping annotated image generation.")
	} else {
		// Draw the observation path on the image
		annotatedImage, err := lightcurve.DrawObservationLineOnImage(displayImage, path)
		if err != nil {
			log.Printf("Could not draw observation line: %v\n", err)
		} else {
			// Save the annotated image
			outputAnnotated := "annotated_diffraction.png"
			err = lightcurve.SaveImageToFile(outputAnnotated, annotatedImage)
			if err != nil {
				log.Printf("Could not save annotated image: %v\n", err)
			} else {
				fmt.Printf("Saved annotated image to %s\n", outputAnnotated)
			}
		}
	}

	fmt.Println("\nDone!")
}

// createTestIntensityMatrix creates a simple test intensity matrix with a diffraction-like pattern
func createTestIntensityMatrix(size int) [][]float64 {
	matrix := make([][]float64, size)
	center := float64(size) / 2.0

	for y := 0; y < size; y++ {
		matrix[y] = make([]float64, size)
		for x := 0; x < size; x++ {
			dx := float64(x) - center
			dy := float64(y) - center
			r := (dx*dx + dy*dy) / (center * center)

			// Create a simple diffraction-like pattern
			if r < 0.1 {
				matrix[y][x] = 0.0 // Shadow
			} else if r < 0.15 {
				matrix[y][x] = 1.2 // Bright ring (diffraction peak)
			} else {
				matrix[y][x] = 1.0 // Normal intensity
			}
		}
	}
	return matrix
}

// createTestGeometricMatrix creates a simple test geometric shadow matrix
func createTestGeometricMatrix(size int) [][]float64 {
	matrix := make([][]float64, size)
	center := float64(size) / 2.0
	radius := float64(size) * 0.15

	for y := 0; y < size; y++ {
		matrix[y] = make([]float64, size)
		for x := 0; x < size; x++ {
			dx := float64(x) - center
			dy := float64(y) - center
			r := dx*dx + dy*dy

			if r < radius*radius {
				matrix[y][x] = 0.0 // Inside the occulter (shadow)
			} else {
				matrix[y][x] = 1.0 // Outside (illuminated)
			}
		}
	}
	return matrix
}
