# Single Point Analysis

## 

## Overview

The Single Point Analysis feature provides a metric for a user to estimate whether a single data point drop in a light curve is likely to be a real occultation event or just a noise artifact. This is particularly useful for assessing the validity of single-point "events" that have large drops --- but is the drop large enough to be reported to Dave Herald and/or Dave Gault? The Daves have a set of tools that they use to accept or reject single point events, but their time is precious. Hopefully this tool will give users a way to self-assess the likelihood that, if submitted for validation, will be found worthy of the Daves efforts.

## 

## How It Works

The analysis uses a 3rd order polynomial fit to establish a baseline reference, then measures how far any selected point deviates from this baseline in terms of standard deviations.

## 

## Step-by-Step Procedure

## 

### Step 1: Define the Baseline Region

Select two points on the light curve to define a baseline region containing the point to be tested:

- Click on the first point to mark one end of the region
- Click on a second point to mark the other end of the region
- Both points must be on the same light curve (of course)
- Choose as wide a region as possible around the point to be tested. This is to swamp out the effect of the point-to-be-tested in increasing the estimated standard deviation of the noise level in the selected region.

## 

### Step 2: Polynomial Fit and Statistics

After selecting two points, the system automatically:

- Fits a 3rd order polynomial (y = a + bx + cx² + dx³) to all points in the baseline region
- Calculates the **mean value** of the data points (for reference only)
- Calculates the **standard deviation** of the residuals (difference between actual points and the polynomial fit)
- Displays the polynomial curve in magenta on the plot

## 

### Step 3: Measure Point Drops

After the baseline is established:

- The selection mode switches to single-point selection
- Click on any point within the baseline region to measure its drop
- Points outside the designated baseline region cannot be selected

## 

For each selected point, the display shows:

- **Actual value**: The measured brightness of the selected point
- **Reference value**: The brightness in the polynomial fit
- **Drop**: The difference between reference and actual (positive = below baseline)
- **Drop / Std Dev**: The drop expressed in standard deviation units (sigma)

## 

## 

## Interpreting Results

The **Drop / Std Dev** ratio is the key metric:

- A ratio of **1.0** means the point is 1 standard deviation below the baseline
- A ratio of **2.0** means 2 standard deviations below
- A ratio of **3.0** or higher suggests the drop is statistically significant

## 

Generally, drops of 3 sigma or more are considered potentially real events, while smaller drops are more likely to be noise artifacts.

## 

## Reset Button

## 

Click the **Reset** button to:

- Clear the polynomial fit and all selections
- Remove the polynomial curve from the plot
- Return to the initial two-point selection mode
- This allows for a new analysis with a different baseline region

## 

## Automatic Cleanup

When you navigate away from the Single Point tab, all selections and the polynomial curve are automatically cleared. This ensures a fresh start each time you return to the tab.

## 

## Tips

- Choose as large a  baseline region around the point to be tested as possible
- A longer baseline region provides more reliable statistics
- The polynomial fit handles gradual brightness trends in the baseline

## 
