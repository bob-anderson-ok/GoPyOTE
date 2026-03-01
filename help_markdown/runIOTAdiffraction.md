# Run IOTAdiffraction

## 

The **Run IOTAdiffraction** button launches the external IOTAdiffraction.exe program to generate diffraction images from an occultation parameters file.

## 

## How It Works ...

## 

### When a CSV light curve has been loaded ...

## 

The program searches the observation folder (the directory containing the loaded light curve CSV) for a `.occparams` file and runs IOTAdiffraction with it — no file dialog is shown.

## 

- If a `.occparams` file is found, it is used immediately.
- If no `.occparams` file is found, an informational message appears asking you to use the **Process occelmnt file** button to create one.

## 

### When no CSV light curve has been selected ...

## 

A message will appear explaining that a light curve must be loaded before proceeding further.

## 

## Camera QE Table Handling

## 

If the selected parameters file specifies a `path_to_qe_table_file` value that does not already include the `CAMERA-QE/` directory prefix, GoPyOTE automatically prepends it before passing the file to IOTAdiffraction.

## 

## IOTAdiffraction Execution

## 

**IOTAdiffraction.exe runs** with the`.occparams` file as its argument, using the application directory as its working directory for image stoage.

## 

**On successful completion:**

## 

- The output dialog displays `[Process completed successfully]`.

## 

- The startup overlay refreshes to show the newly generated `diffractionImage8bit.png` and the event title from the parameters file.

## 

**On failure:** The output dialog displays the error message from the process.

## 

## Prerequisites

## 

- **IOTAdiffraction.exe** must be located in the same directory as the GoPyOTE executable. If it is not found, an informational dialog shows the expected path.

## 

## Generated Files

## 

IOTAdiffraction.exe produces the following files in the application directory:

## 

## diffractionImage8bit.png

## 

8-bit diffraction image used for display and observation path overlays. It has auto-contrast for easy viewing and is not used for 'science'.

## 

## targetImage16bit.png

## 

16-bit diffraction image used for light curve extraction and fitting. No auto-contrast so values are correct for 'science'.

## 

## geometricShadow.png

## 

Geometric shadow image. Used to define physical edges of objects.

## 

## Typical Workflow

## 

1. Use **Process occelmnt file** to create a `.occparams` file in your observation folder, then edit it via **Edit Occultation Parameters** as needed.
2. Load your light curve CSV.
3. Click **Run IOTAdiffraction** — the `.occparams` file in the observation folder is found and used automatically.
4. Wait for the process to complete. The diffraction image display confirms success.
5. Use the Fit tab to align the observed light curve against the generated diffraction pattern.

## 
