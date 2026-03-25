package main

import "image/color"

// EllipseParams represents ellipse parameters for the main body or satellite
type EllipseParams struct {
	XCenterKm          float64 `json:"x_center_km"`
	YCenterKm          float64 `json:"y_center_km"`
	MajorAxisKm        float64 `json:"major_axis_km"`
	MinorAxisKm        float64 `json:"minor_axis_km"`
	MajorAxisPaDegrees float64 `json:"major_axis_pa_degrees"`
}

// OccultationParameters holds all parameters for occultation calculations
type OccultationParameters struct {
	WindowSizePixels               int           `json:"window_size_pixels"`
	Title                          string        `json:"title"`
	FundamentalPlaneWidthKm        float64       `json:"fundamental_plane_width_km"`
	FundamentalPlaneWidthNumPoints int           `json:"fundamental_plane_width_num_points"`
	ParallaxArcsec                 float64       `json:"parallax_arcsec"`
	DistanceAu                     float64       `json:"distance_au"`
	PathToQeTableFile              string        `json:"path_to_qe_table_file"`
	ObservationWavelengthNm        int           `json:"observation_wavelength_nm"`
	DXKmPerSec                     float64       `json:"dX_km_per_sec"`
	DYKmPerSec                     float64       `json:"dY_km_per_sec"`
	PathPerpendicularOffsetKm      float64       `json:"path_perpendicular_offset_from_center_km"`
	PercentMagDrop                 int           `json:"percent_mag_drop"`
	StarDiamOnPlaneMas             float64       `json:"star_diam_on_plane_mas"`
	LimbDarkeningCoeff             float64       `json:"limb_darkening_coeff"`
	StarClass                      string        `json:"star_class"`
	MainBody                       EllipseParams `json:"main_body"`
	Satellite                      EllipseParams `json:"satellite"`
	PathToExternalImage            string        `json:"path_to_external_image"`
	ExposureTimeSecs               float64       `json:"exposure_time_secs"`
	OccelmntXml                    string        `json:"occelmnt_xml,omitempty"`
}

// LightCurveColumn represents a single light curve column from a CSV file
type LightCurveColumn struct {
	Name   string
	Values []float64
}

// LightCurveData holds all parsed data from a light curve CSV file
type LightCurveData struct {
	TimeValues     []float64          // Decoded timestamps as float64 seconds
	FrameNumbers   []float64          // Frame numbers from the first column (used when timestamps empty)
	Columns        []LightCurveColumn // All data columns (excluding index and time)
	SkippedLines   []string           // Comment and blank lines preserved for writing output
	HeaderLine     string             // Original header line for writing output
	SourceFilePath string             // Path to the original CSV file
}

// PlotPoint represents a data point in the light curve
type PlotPoint struct {
	X            float64 // Time or frame number
	Y            float64 // Brightness/magnitude
	Index        int     // Point index for identification
	Series       int     // Which series this point belongs to
	Interpolated bool    // True if this point was interpolated (e.g., dropped frame)
}

// PlotSeries represents a single light curve series
type PlotSeries struct {
	Points        []PlotPoint
	Color         color.RGBA
	Name          string
	LineOnly      bool    // If true, draw line only — no scatter dots
	ScatterOnly   bool    // If true, draw scatter dots only — no line
	ScatterRadius float64 // Custom scatter dot radius in points; 0 means default (4)
}

// PointPair represents a pair of selected points (for multi-pair selection mode)
type PointPair struct {
	Point1SeriesIdx int
	Point1Idx       int
	Point1DataIdx   int
	Point1Frame     float64
	Point1Value     float64
	Point1Series    string
	Point2SeriesIdx int
	Point2Idx       int
	Point2DataIdx   int
	Point2Frame     float64
	Point2Value     float64
	Point2Series    string
}
