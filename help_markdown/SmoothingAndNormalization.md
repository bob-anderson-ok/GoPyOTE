# Smoothing and Normalization

## The problem that is addressed by this feature

The lightcurves in an observation can exhibit gradual changes in intensity. The list below gives some likely causes for these intensity changes. It is not meant to be exhaustive but it identifies common sources:

- Haze/fog building up or going away

- the moon is coming into view or is moving out of view

- sundown or sunrise is approaching

- your neighbors smoldering burn pile smoke appears due to a wind shift

- ...

Figure 1 below gives an example where removing such effects was important. There actually is a broad, low magDrop event in the target lightcurve, but it's hard to pick out because of the drooping at the righthand side.

## 

![Figure 1: Note that haze is moving in - tracking star drooping](help_images/SmoothingFig1.png =800x600)

## 

## 

What we do is calculate a smooth curve through the tracking star lightcurve using a second degree Savitsky-Golay smoother. That routine needs to know which curve to use and how big the window should be (that is, how many points at a time should be inluded in the least squares fit of a second degree function). By selecing two points on the reference curve (here that is the tracking star curve) we specify both the curve to use and the window size. Figure 2 shows the selections I chose. You want the window to be wide enough that the smooth curve is not following scintillations, but not so wide that the light curve variation is not being tracked well.

## 



![Figure 2: Selection of smoothing window on tracking star light curve](help_images/SmoothingFig2.png =800x600)

## 

Next we trigger the computation of the smoothed curve by clicking on the **Smooth** button resulting in Figure 3:

## 

![Figure 3: Smoothing curve generated](help_images/SmoothingFig3.png =800x600)

## 

Finally, we click the **Normalize** button to reach Figure 4. Now the broad low magDrop event is easier to see/find.

## 

![Figure 4: Normalization applied](help_images/SmoothingFig4.png =800x600)
