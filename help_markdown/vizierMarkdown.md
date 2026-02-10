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

- **Number**: The asteroid's catalog number (max 6 digits)
- **Name**: The asteroid's name

## 

### Output Folder

The destination folder for generated .dat files. Defaults to Documents/VizieR.

## 

## Auto-Fill Options

The VizieR fields can be automatically populated from several sources:

## 

### RAVF File Headers

When loading a CSV file exported from an RAVF (Astrid) recording, the fields are automatically filled from embedded metadata.

## 

### ADV File Headers

When loading a CSV file exported from an **ADV** recording, available metadata is extracted and used to fill the fields. Note: **Tangra** apparently does not include the ADV meta-data in the csv file headers.

## 

### NA Spreadsheet

Click **Load from NA spreadsheet** to import data from a North American Asteroid Occultation Report Form (.xlsx file).

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

## Workflow

## 

1. Fill in all required fields (or use autofill)
2. Click **Generate VizieR .dat file**
3. Repeat for additional observations if needed
4. Click **Zip *.dat files for sending** to create a zip archive
5. Email the zip file to [HeraldDR@bigpond.com]([HeraldDR@bigpond.com](mailto:HeraldDR@bigpond.com))

## 

Note: [HeraldDR@bigpond.com](mailto:HeraldDR@bigpond.com) is NOT Dave Herald's 'regular' email address but rather a special one for receiving these reports!

The Subject line of the email must be (to avoid being treated as spam)

    **Light curve report**

There is no need to say anything in the body of the email,  but if you do so,  include the word **attachment**.  Some email clients will notice that word AND the fact that you didn't actually add any attachment and warn you about that!

You should receive an automatic acknowledgement of your email in a few minutes.

If there are any problems with the content of your report,  you will be notified when the next batch of light curve reports are being processed.  This may not be for several weeks or more.

Your light curve will be visible within Occult4 after it has been vetted - this may take a couple of weeks.  It will later be archived at Vizier in Catalogue B/occ,  which can be accessed at[ https://vizier.u-strasbg.fr/viz-bin/VizieR?-source=B/occ](https://vizier.u-strasbg.fr/viz-bin/VizieR?-source=B/occ)

## 

## Notes

## 

- The .dat files use CRLF line endings for compatibility
- After zipping, the original .dat files are automatically deleted (but they remain recoverable as they are in the zip folder just created)
- In addition, a copy is always saved in the associated RESULTS folder.
- The zip filename includes a timestamp for uniqueness

## 
