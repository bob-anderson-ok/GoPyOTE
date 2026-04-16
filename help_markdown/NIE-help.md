# Noise-Induced Event (NIE) Analysis
##
## What is NIE?
##
**NIE** stands for *Noise-Induced Event*. GoPyOTE will always find a possible event in
any light curve you give it — but that does not mean an event actually occurred. The
NIE analysis is the tool that helps you decide whether the drop you are looking at
is a genuine occultation, or just a statistical fluctuation of the baseline noise
that happens to look like one.
##
The question NIE answers is simple:
##
*If there were no event at all — just noise — how big a drop could the noise alone
produce? And is the drop we actually observed bigger than that, or not?*
##
## How the NIE process works
##
The process is a Monte Carlo experiment built on the noise characteristics that
GoPyOTE has already measured from your baseline:
##
1. **Measure the baseline.** During normalization, GoPyOTE estimates the baseline
   noise sigma and fits an autoregressive (AR) model from the lag coefficients
   that describe the chunkiness of your noise. (See *Correlated Noise*
   for details.) Correlated noise is then used for the trials when both of the
   following are true: the *Use correlated noise* checkbox on the Settings tab
   is checked (it is by default), **and** the AR model fit from the baseline
   succeeded. If either condition fails, the trials fall back to white noise.
##
2. **Determine the event window width.** The width of the event — in samples — is
   taken from the geometric shadow edge times found by the fit. This is the width
   of the sliding window used in every trial.
##
3. **Run many trials (typically 10,000).** For each trial:
   - Generate a synthetic baseline of the same length as your observation,
     drawn either from the AR model that reproduces the correlated (chunky)
     noise of your actual baseline, or — if correlated noise is not in use —
     from white Gaussian noise (mean 1.0, measured sigma).
   - Slide a window of the event width across that synthetic baseline and
     record the *minimum* window mean — i.e., the deepest dip the noise alone
     managed to produce in that trial.
##
4. **Build the NIE distribution histogram.** The collection of minimum window means from
   all trials forms the NIE distribution: the distribution of the deepest
   noise-only drops you would expect to see in an observation of this length,
   with this noise, and a window this wide.
##
5. **Compare to the observed event.** GoPyOTE then draws a vertical line on the
   plot at the actual observed event drop (from the fit), so you can see
   at a glance where your event falls relative to the noise distribution (the green histogram).
##
## Reading the NIE plot
##
![NIE plot](help_images/NIE-LB11-demo.png =800x600)
##
The NIE histogram window is opened from the *Fit* tab. It contains:
##
- **X-axis — Drop position.** The minimum window mean per trial. Values near 1.0
  mean the noise produced almost no dip; values farther to the right mean the
  noise produced a bigger dip. *Drops get bigger toward the right.*
##
- **Y-axis — Density.** How often each drop size occurred across the trials.
##
- **Green histogram bars.** The distribution of noise-only drops.
##
- **Blue vertical line — observed event drop.**  This is the drop you are asking about.
##
- **Red vertical line — predicted magnitude drop.** Drawn only if a predicted
  magnitude drop is available from `details.csv`. This shows where an event of
  the predicted brightness drop would appear.
##
- **Title.** Includes the occultation name, the number of trials, and whether
  correlated or white noise was used — e.g., *EY18 — NIE Analysis (10000 trials,
  AR Correlated Noise)*.
##
## How to interpret the result
##
Look at where the **blue line** falls relative to the green histogram:
##
- **Blue line well to the right of the histogram** — the observed drop is bigger
  than anything the noise alone produced in 10,000 trials. The event is
  statistically significant - very unlikely to be a product of noise.
##
- **Blue line sitting inside the histogram** — the observed drop is the same
  size as drops that noise alone routinely produces. The event is *not*
  statistically distinguishable from noise, and should not be reported as a
  positive. 
##
- **Blue line to the left of the histogram** — the observed drop is smaller
  than typical noise-only drops. This essentially never happens for a real
  event and usually means the fit is not picking up a real feature.
##
## Manual NIE point selection
##
The *Enable NIE point(s) selection* checkbox on the Fit tab switches the NIE
analysis from its default **fit-derived** mode into a **manual selection**
mode, in which you choose the point or region on the light curve that the NIE
analysis should evaluate.
##
- **Unchecked (default) — fit-derived mode.** The NIE window width and the
  observed event drop are taken from the fit result: the window width is the
  number of samples between the detected geometric-shadow edges.
  This is the normal flow after a successful fit.
##
- **Checked — manual selection mode.** Clicks on the light curve no longer
  build baseline pairs; instead they set the first and second NIE points
  directly, and any previously-marked points are cleared so you start fresh.
  The *Run NIE analysis* button is also enabled, so you can run the study
  even if no fit result is available. Select one or two points on the light
  curve, then click *Run NIE analysis*:
##
  - **One point selected** — the NIE window width is 1, and the observed
    event drop is the *value* of the selected point. Use this to ask
    whether a single dipped sample could plausibly have come from the
    noise alone.
##
  - **Two points selected** — the NIE window width is the number of
    observed samples between the two selected frames (inclusive), and the
    observed event drop is the *mean* of those samples. Use this to ask
    whether a sustained dip over a specific region of the light curve
    could plausibly have come from the noise alone.
##
The NIE histogram window title reports which mode was used — *fit-derived*
or *manual selection* — so there is no ambiguity about what was evaluated.
Unchecking the box restores normal multi-pair baseline selection.
##
## Why correlated noise matters
##
If you run the NIE analysis with white noise when your actual noise is
correlated, the NIE distribution will be too narrow and your event will look
more significant than it really is. Scintillation produces correlated
(chunky) noise, and chunky noise routinely produces bigger minimum-window
drops than white noise does. The *Use correlated noise* checkbox on the
Settings tab defaults to **checked**, and this is the setting that should
normally be used. Switch it off only for experimentation. Note that even
with the checkbox on, GoPyOTE falls back to white noise automatically if
the AR model could not be fit from the baseline — the title of the NIE
histogram always tells you which kind of noise was actually used.
##
## Important assumptions and caveats
##
- **The baseline noise is representative of the event window.** NIE assumes
  the noise you measured in the baseline is also the noise that was present
  during the event. If seeing changed, or the baseline contains unstable
  stretches, this assumption weakens.
##
- **The correlated-noise model needs enough baseline points.** The
  autoregressive model uses lag coefficients measured out to lag 10; a short
  or noisy baseline gives unreliable coefficients and therefore an unreliable
  NIE distribution.
##
- **The event window width must be correct.** The window width comes from the
  geometric shadow edges. If the edges are mislocated or the observation path
  offset is wrong, the window width is wrong and the NIE result is
  correspondingly wrong.
##
- **NIE is a statistical aid, not a verdict.** It tells you whether the drop
  could plausibly have come from noise alone. The decision to report a
  positive, a negative, or *miss* still belongs to the observer and depends
  on physics, geometry, and context beyond what NIE measures.
##
