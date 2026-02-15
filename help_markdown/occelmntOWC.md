# Process OWC occelmnt file

The **Process OWC occelmnt file** button, located at the bottom of the main window, opens a dialog for computing observer-relative shadow velocities from an OWC (Occult Watcher Cloud) occelmnt XML file.

##

## Dialog Layout

The dialog contains the following sections from top to bottom:

##

### Paste Area

A large multi-line text box where you paste the contents of an OWC occelmnt XML file using Ctrl+V. The text box is auto-focused when the dialog opens so you can paste immediately.

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

### Buttons

- **Load site file** — Opens a file dialog in the `SITE-FILES` directory (created automatically if it does not exist), showing only `.site` files. Automatically fills the Site Location fields (latitude, longitude, altitude) from the selected file.

- **Write site file** — Opens a file save dialog in the `SITE-FILES` directory (created automatically if it does not exist) to write the current Site Location fields (latitude, longitude, altitude) to a `.site` file. The `.site` extension is enforced automatically.

- **Calculate observer dX dY (closes dialog)** — Computes the observer-relative shadow velocity, creates a diffraction parameters file, closes this dialog, and opens the Edit/Enter Occultation Parameters dialog. A Fresnel scale information popup is also displayed. See workflow below.

- **Cancel** — Closes the dialog without performing any calculation.

##

## Workflow

1. **Paste the occelmnt XML** into the text box (Ctrl+V).

2. **Enter your observer location.** Either type the coordinates manually, or click **Load site file** to load them from a previously saved `.site` file. You can save your current location using **Write site file** for future use.

3. Click **Calculate observer dX dY (closes dialog)**. This will:

   - Call `ShadowVelocityFromOWCEventKmPerSec` to compute the relative shadow velocity (vx, vy in km/s) for your observer location, accounting for Earth rotation via WGS-84 geodesy and Earth Rotation Angle.
   - Parse the `<Object>` element from the XML to extract:
     - **Index 0, 1:** asteroid number and name, used to set the `title` field as `(number) name`
     - **Index 3:** body diameter in km, used to set `major_axis_km` and `minor_axis_km`
     - **Index 4:** distance in AU, used to set `distance_au`
   - Create a parameters file named `from_occelmnt` in the application directory with:
     - `title` — from the asteroid number and name
     - `window_size` — defaulted to 600
     - `dX_km_per_sec` — computed vx
     - `dY_km_per_sec` — computed vy
     - `distance_au` — from the XML
     - `fundamental_plane_width_km` — 3x the body diameter, rounded up
     - `fundamental_plane_width_num_points` — defaulted to 2000
     - `observation_wavelength_nm` — defaulted to 550
     - `main_body.major_axis_km` and `main_body.minor_axis_km` — body diameter
   - Close this dialog and open the **Edit/Enter Occultation Parameters** dialog with the `from_occelmnt` file loaded, so you can review, adjust, and save the parameters before running IOTAdiffraction.
   - Display a **Fresnel Scale** popup showing:
     - The Fresnel scale in km and meters
     - The wavelength and distance used for the calculation
     - Samples per Fresnel scale (calculated from the fundamental plane parameters)
     - Guidance that 5-6 samples per Fresnel scale are needed as a minimum for observations exhibiting diffraction effects

   ##

## Notes

- The calculation uses `dut1Seconds = 0`, `xpArcsec = 0`, and `ypArcsec = 0` by default. For sub-10 ms precision work, you would need actual UT1-UTC and polar motion values from the IERS.
- The `from_occelmnt` file is overwritten each time you click Calculate. To preserve a set of parameters, use the Write button in the Parameters dialog to save to a named file (with `.occparams` extension).
- Both **Load site file** and **Write site file** always open in the `SITE-FILES` directory relative to the application directory.

##
