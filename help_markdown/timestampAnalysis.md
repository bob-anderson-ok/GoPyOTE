# Dropped frames and OCR error detection

GoPyOTE automatically analyzes timestamps when a CSV file
is loaded to detect timing anomalies that will affect occultation timing accuracy.

### 

### Dropped Frames

When video frames are lost during recording, there will be gaps in the 
timestamp sequence. GoPyOTE detects these by looking for 
time steps that are 1.8x or greater than the median 
time step. For example, if your camera runs at 29.97 fps
(0.033367 seconds per frame), a gap of 0.0660 seconds or more indicates at least one dropped frame.

**How it's handled:** The valid points at the beginning and end of the dropped frame sequence are used
to calculate good guesses for the missing points by linear interpolation.

**Visual indicator:** Interpolated points appear as **dark gray circles with a red outline** on the plot.

### 

### OCR Errors (Negative Time Deltas)

When timestamps are read via OCR from video frames, 
occasionally a digit will be misread. If that digit change causes a timestamp
to be calculated as earlier in time than the previous frame,
a negative time delta will 
result. 
If this is an isolated event (only one frame affected and neighbors are correct), it is correctable.

**How it's handled:** The erroneous timestamp is replaced with the expected value (previous timestamp + average time step).

**Visual indicator:** Points with corrected OCR errors appear with a **black circle outline** around the normal series color.

### 

### Cadence Errors

These are timing irregularities where the time step deviates significantly from normal (30% faster or slower) but isn't large enough to indicate a dropped frame. These may indicate:

- Minor OCR errors that don't cause negative deltas
- Camera timing jitter
- Recording software issues

**How it's handled:** These are reported but not automatically corrected, as they may represent real timing variations.

## 

## Timing Analysis Report

When timing errors are detected, a dialog appears showing:

- **Median time step** - The typical time between frames
- **Average time step** - Calculated from valid (non-dropped) frames only
- **Negative delta errors** - Frames where OCR likely misread the timestamp
- **Cadence errors** - Frames with unusual but not severe timing
- **Dropped frame errors** - Gaps where frames were lost

## 

## Example Plot

The image below shows how dropped frames and OCR errors appear on a light curve:

![Plot showing dropped frames and OCR error markings](help_images/droppedFrameDemoPlot.png%20=800x600)

## 

## Limitations

- **Consecutive OCR errors:** The algorithm cannot reliably fix OCR errors on consecutive frames, as there's no valid reference point between them. See the example plot below to see what happens in this case.
- **Forward time jumps from OCR:** If an OCR error causes time to jump forward, the jump size may be so large as to be equivalent to dropped frames. This situation cannot be distinguished automatically.

## 

## Consecutive OCR error example

The image below shows how consecutive OCR errors will appear.
Whenever you see points out of order in time like this, you will need to edit the csv file manually.
Click on the points identified as having OCR errors and use the frame numbers 
found there to navigate in the csv file.

**Note:** the consecutive OCR errors in the above example also caused the erroneous detection of dropped frames a little later in the plot.

## 

![Plot showing effect of consecutive OCR misreads](help_images/consecutiveOCRerrorDemo.png =800x600)