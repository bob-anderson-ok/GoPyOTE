# Edit/Enter Occultation Parameters

The **Edit/Enter Occultation Parameters** dialog is used to create and edit the JSON5 parameter files that drive the IOTAdiffraction external diffraction-image generator. It is opened by clicking the **Edit/Enter Occultation Parameters** button at the bottom of the main window, or automatically after completing the **Process occelmnt file** dialog.

## 

## Buttons

- **Browse** — Opens a file dialog to select and load an existing `.occparams` parameters file. All fields are populated from the file. The file path is remembered for future sessions. Only `.occparams` files are shown in the file dialog.

- **Write** — Opens a file save dialog to write the current field values to a parameters file. See **Writing a Parameters File** below for important details.

- **Show associated occelmnt.xml** - Opens a dialog panel to show the associated occelmnt file.

- **Cancel** — Closes the dialog. If any field has been modified since the last load or save, a confirmation prompt appears warning about unsaved changes.

## 

## Writing a Parameters File

When you click **Write**, a call is made to the IOTAdiffraction application, which uses the just completed occultation parameters file to generate the diffraction images.

## 

## Auto-Load Behavior

When the dialog opens, if there has been a previous processing of this observation which created an occultation parameters file, it is automatically opened. This means that any edits (shape changes, resolution changes, etc.) will be persisted - edits will not be lost.



## 

## File Format

Parameters files use JSON5 format (with `.occparams` extension), which is JSON with relaxed syntax (trailing commas allowed, comments supported). A typical file looks like:

```json5
{
  "title": "(9203) Myrtus 2025 Feb 22",
  "window_size_pixels": 600,
  "fundamental_plane_width_km": 40,
  "fundamental_plane_width_num_points": 2000,
  "distance_au": 2.33,
  "dX_km_per_sec": 5.074,
  "dY_km_per_sec": -0.904,
  "observation_wavelength_nm": 550,
  "percent_mag_drop": 75,
  "exposure_time_secs": 0.2,
  "limb_darkening_coeff": 0.7,
  "path_to_qe_table_file": "qhy174m.qe",
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

## 

**Name of camera QE file** — if this field contains a filename (e.g., `qhy174m.qe`), GoPyOTE automatically prefixes it with `CAMERA-QE/` when running IOTAdiffraction, so the file is resolved as `CAMERA-QE/qhy174m.qe` relative to the application directory. The original parameters file is not modified. A drop down selection list is used to assist in error-free file selection. In addition, the selected value is persisted - only if you have more than one camera will this value need to be changed from observation to observation.

## 
