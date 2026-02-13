# Edit/Enter Occultation Parameters

The **Edit/Enter Occultation Parameters** dialog is used to create and edit the JSON5 parameter files that drive the IOTAdiffraction external diffraction-image generator. It is opened by clicking the **Edit/Enter Occultation Parameters** button at the bottom of the main window, or automatically after computing shadow velocities from the **Process OWC occelemnt file** dialog.

## 

## Buttons

- **Browse** — Opens a file dialog to select and load an existing JSON5 parameters file. All fields are populated from the file. The file path is remembered for future sessions.

- **Write** — Opens a file save dialog to write the current field values to a JSON5 parameters file. The save dialog defaults to the same directory and filename as the last loaded file. After a successful save, the dialog closes automatically.

- **Cancel** — Closes the dialog. If any field has been modified since the last load or save, a confirmation prompt appears warning about unsaved changes.

## 

## Auto-Load Behavior

When the dialog opens, it automatically loads the most recently used parameters file (persisted across sessions). This means you can close and reopen the dialog without losing your last set of parameters.

The **Process OWC occelemnt file** dialog also creates a parameters file (named `from_occelemnt`) and sets it as the auto-load target before opening this dialog, so computed values appear pre-filled.

## 

## File Format

Parameters files use JSON5 format, which is JSON with relaxed syntax (trailing commas allowed, comments supported). A typical file looks like:

```json5
{
  "title": "(9203) Myrtus 2025 Feb 22",
  "window_size_pixels": 800,
  "fundamental_plane_width_km": 40,
  "fundamental_plane_width_num_points": 2000,
  "distance_au": 2.33,
  "dX_km_per_sec": 5.074,
  "dY_km_per_sec": -0.904,
  "observation_wavelength_nm": 500,
  "percent_mag_drop": 75,
  "exposure_time_secs": 0.2,
  "limb_darkening_coeff": 0.7,
  "star_class": "K",
  "main_body": {
    "x_center_km": 5.8,
    "y_center_km": 0.6,
    "major_axis_km": 17.6,
    "minor_axis_km": 8,
    "major_axis_pa_degrees": 98.3,
  },
  "satellite": {
    "major_axis_km": 7.5,
    "minor_axis_km": 2.8,
    "major_axis_pa_degrees": 94,
    "x_center_km": -5.8,
    "y_center_km": -0.6,
  },
}
```

## 

## Usage with IOTAdiffraction

After editing and saving a parameters file, use the **Run IOTAdiffraction** button to generate diffraction images. The Run IOTAdiffraction button will prompt you to select a parameters file; it defaults to the most recently loaded one. Upon completion, the generated diffraction image and title are displayed as a splash overlay in the main window.

## 
