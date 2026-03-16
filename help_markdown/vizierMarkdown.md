# VizieR Export

## 

## Overview

The VizieR tab allows you to export light curve data in a format suitable for submission to the VizieR astronomical database. VizieR is the standard repository for occultation light curves maintained by the astronomical community.

## 

## Prerequisites

Before using the VizieR export:

1. Load a light curve file using the **.csv ops** tab
2. Select exactly **one** light curve for export
3. Set the **Start Frame** and **End Frame** values to trim the data to approximately 100 points surrounding the event (if there is an excess of points on either side of the event).

## 

## Workflow

1. If the fields are not already filled, click **Copy from SODIS-REPORT.txt***
2. Click **Preview submission**
3. Click **Generate VizieR .dat file** to write the .dat file into the **-RESULTS** folder 

## 

## Input Fields

## 

### Date

Enter the observation date (Year, Month, Day) when the occultation event occurred.

## 

### Star Catalog ID

Enter **exactly one** star catalog identifier. Supported catalogs:

- **UCAC4**: Format xxx-xxxxxx (e.g., 123-456789)
- **Tycho-2**: Format xxxx-xxxxx-x (e.g., 1234-56789-1)
- **Hipparcos**: Numeric ID only

## 

Best practice is to use the star designation from the Occult4 prediction.

## 

### Site Location

- **Longitude**: Degrees, minutes, seconds (use negative for West)
- **Latitude**: Degrees, minutes, seconds (use negative for South)
- **Altitude**: Elevation in meters above sea level

## 

### Observer

Your name as it should appear in the database.

## 

### Asteroid

- **Number**: The asteroid's catalog number (max 6 digits). **Note: if the asteroid number is given as 0, a dash is substitued**
- **Name**: The asteroid's name (if available)

## 

## Output Format

## 

The generated .dat file contains five lines:

## 

1. **Date**: Observation date, start timestamp, duration, and reading count
2. **Star**: Catalog identifiers for the occulted star
3. **Observer**: Location coordinates and observer name
4. **Object**: Asteroid number and name
5. **Values**: Light curve readings scaled to 0-9524 range

## 

Dropped frames are represented as empty values in the output (a single space is used as the *value*).

## 

## Note

## 

- The .dat files use CRLF line endings for compatibility

## 
