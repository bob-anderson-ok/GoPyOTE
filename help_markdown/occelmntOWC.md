# Process OWC occelmnt file

The **Process OWC occelmnt file** button, located at the bottom of the main window, opens a dialog for computing observer-relative shadow velocities from an OWC (Occult Watcher Cloud) occelmnt XML file.

## 

## Dialog Layout

The dialog contains the following sections from top to bottom:

## 

### Paste Area

A large multi-line text box where you paste the contents of an OWC occelmnt XML file using Ctrl+V. The text box is auto-focused when the dialog opens so you can paste immediately. When a paste is performed, the xml file is automatically saved to the **-RESULTS** folder and will be used the next this observation is opened for processing.

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

- **Cancel** — Closes the dialog without performing any calculation.

## 

## Workflow

1. **Paste the occelmnt XML** into the text box (Ctrl+V). Alternatively, click the **Load Occelmnt file** button to open a browser to find an xml file that is saved somewhere else. This allows for alternate workflows for occelmnt.xml file access.

2. **Enter your observer location.** Either type the coordinates manually, or click **Load site file** to load them from a previously saved `.site` file. You can save your current location using **Write site file** for future use.

3. Click **Create Occultation Parameter file (closes dialog)**. This will:
   
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
   - Thedialog closes and opens in a new dialog: **Edit/Enter Occultation Parameters** dialog with the `from_occelmnt` file loaded, so you can review, and adjust if needed., and after you click on **Write**, IOTAdiffraction will be called
   - Display a **Fresnel Scale** popup showing:
     - The Fresnel scale in km and meters
     - The wavelength and distance used for the calculation
     - Samples per Fresnel scale (calculated from the fundamental plane parameters)
     - Guidance that 5-6 samples per Fresnel scale are needed as a minimum for observations exhibiting diffraction effects
   - Click on **Write**, and IOTAdiffraction will be called.
   
   ## 

## Notes

- Both **Load site file** and **Write site file** always use the `SITE-FILES` directory relative to the application directory.

## 
