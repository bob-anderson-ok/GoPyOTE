# Process OWC occelemnt file

The **Process OWC occelemnt file** button, located at the bottom of the main window, opens a dialog for computing observer-relative shadow velocities from an OWC (Occult Watcher Cloud) occelemnt XML file.

## 

## Dialog Layout

The dialog contains the following sections from top to bottom:

## 

### Paste Area

A large multi-line text box where you paste the contents of an OWC occelemnt XML file using Ctrl+V. The text box is auto-focused when the dialog opens so you can paste immediately.

## 

### Site Location

Enter the observer's geographic coordinates. You can enter values in either format and the other will update automatically:

- **Longitude(DMS):** degrees, minutes, seconds
- **Longitude(degrees):** decimal degrees
- **Latitude(DMS):** degrees, minutes, seconds
- **Latitude(degrees):** decimal degrees
- **Altitude (m):** altitude in meters

Use negative degrees for West longitude and South latitude.

## 

### Asteroid

- **Number:** the asteroid number (e.g. `9203`)
- **Name:** the asteroid name (e.g. `Myrtus`)

## 

### Buttons

- **Fill from SODIS form** — Opens a file dialog to select a SODIS `.txt` form file. Automatically fills the Site Location fields (longitude, latitude, altitude) and Asteroid fields (number, name) from the SODIS form.

- **Calculate observer dX dY** — Computes the observer-relative shadow velocity and creates a diffraction parameters file. See workflow below.

## 

## Workflow

1. **Paste the occelemnt XML** into the text box (Ctrl+V).

2. **Enter your observer location.** Either type the coordinates manually, or click **Fill from SODIS form** to load them from a SODIS observation form.

3. Click **Calculate observer dX dY**. This will:
   
   - Call `ShadowVelocityFromOWCEventKmPerSec` to compute the relative shadow velocity (vx, vy in km/s) for your observer location, accounting for Earth rotation via WGS-84 geodesy and Earth Rotation Angle.
   - Parse the `<Object>` element from the XML to extract:
     - **Index 0, 1:** asteroid number and name, used to set the `title` field as `(number) name`
     - **Index 3:** body diameter in km, used to set `major_axis_km` and `minor_axis_km`
     - **Index 4:** distance in AU, used to set `distance_au`
   - Create a parameters file named `from_occelemnt` in the application directory with:
     - `title` — from the asteroid number and name
     - `dX_km_per_sec` — computed vx
     - `dY_km_per_sec` — computed vy
     - `distance_au` — from the XML
     - `fundamental_plane_width_km` — 3x the body diameter, rounded up
     - `fundamental_plane_width_num_points` — defaulted to 2000
     - `observation_wavelength_nm` — defaulted to 550
     - `main_body.major_axis_km` and `main_body.minor_axis_km` — body diameter
   - Open the **Edit/Enter Occultation Parameters** dialog with the `from_occelemnt` file loaded, so you can review, adjust, and save the parameters before running IOTAdiffraction.
   
   ## 

## Notes

- The calculation uses `dut1Seconds = 0`, `xpArcsec = 0`, and `ypArcsec = 0` by default. For sub-10 ms precision work, you would need actual UT1-UTC and polar motion values from the IERS.
- The `from_occelemnt` file is overwritten each time you click Calculate. To preserve a set of parameters, use the Write button in the Parameters dialog to save to a named file.
- The SODIS form directory preference is shared with the VizieR tab's "Load from SODIS form" button.

## 
