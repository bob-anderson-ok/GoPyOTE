# Fresnel Scale Resolution

## 

Fresnel diffraction effects appear in an observed light curve as spikes at the D and R edges that are approximately 1.4 times the average baseline value. There may also be a noticeable gradual transition at D and R and even a barrel shaped bottom that fails to reach the zero level. If this is the case, then it is important that the resolution of the diffraction image be fine enough that 5 or 6 (or a little more) sample points span a Fresnel scale. The sharp spike and slope regions (D and R zones) of a diffraction curve occur over about 1.5 Fresnel scale (length in km), so to get good theoretical light curves for fitting, this region needs to be reasonably well-sampled. That's why GoPyOTE gives you a report on how well your diffraction image samples the diffraction effects.

## 

## Some reasons you might not see diffraction effects

## 

- The target star has a finite size projected at the asteroid plane. When present, this effect often dominates the shape of the light curve and diffraction produces only secondary effects, usually showing up as just a little blip up at the D and R transitions.

- The camera exposure time is not small enough to track the D and R changes (most common). Effectively the camera is integrating the light curve and smoothing away diffraction effects.

## 

## What to do if diffraction effects are evident but the sampling is less than 5 points per Fresnel scale.

## 

The only way to increase the resolution (make it finer to sample more points per Fresnel scale) is by increasing the parameter called **Fund. Plane Width (pts)** in the Occultation parameters file. The program defaults this value to 2000, a number that will probably be enough for most situations. If not, this number can be increased, but here's the caveat: execution time increases by the cube of the number of points across the width of the Fundamental plane. (Memory usage is 16*points^3 bytes, so this can become an obstacle as well.) The default of 2000 points has very acceptable execution times for modern computers, even when applying a multi-wavelength camera response file. For example, if you change 2000 to 4000, it will take 8 times as long to compute. A practical limit is probably 6000 points and in that case, you should avoid using a camera response curve as the calculation has to be repeated at each wavelength in that table.

## 
