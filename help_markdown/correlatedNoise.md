# Correlated Noise Effects
##
## Introduction
##
Stellar occultation light curves include noise from four main sources:
##
- Scintillation noise
- Photon noise
- Sky noise
- Read noise
##
Scintillation noise is often the dominant source of noise in stellar occultation light curves.
While the other noise sources are white noise – which means that the amplitude of frame-to-frame noise is
unrelated to the noise values in previous frames – scintillation noise is **NOT** white noise.
##
Scintillation noise always exhibits some degree of *correlation*, which means that the amplitude of
the noise in a given frame is loosely connected to the noise in previous frames. The
term for this is **correlated noise**. It exhibits itself visually as *chunkiness* in the
light curve. Another way to think about the situation is that when correlated noise is present, it becomes
possible to predict with some accuracy the noise in frame N from the noise in some number of
preceding frames.
##
GoPyOTE forms such a noise predictor by measuring the correlation coefficients out to 
lag 10 and uses those values to form a *predictor* (which looks backward 10 frames) that 
can then be applied 
to white noise and turn it into
correlated (chunky) noise. This is the correct noise to use for the statistical operations
of determing edge timing error bars (using Monte Carlo trials that renoise the solution
and refind the edges), and to compare the observed event drop to what might
be expected if there were no event but just noise – the *noise-induced-event* (NIE)
analysis.
##
The following figure shows a possible event. Note: GoPyOTE will always find a possible event. It
is up to the observer to decide whether the event should be reported as a POSITIVE. But is
this one really an event? That question is what the NIE analysis is designed to help answer.
![Could this be a possible event ???](help_images/PossibleEvent.png =800x600)

##
The following figure shows the NIE distribution that results from using white noise ...

![This NIE plot shows a statistically valid event](help_images/NIEusingUncorrelatedNoise.png =800x600)
But this shows that the event drop is bigger than the maximum drop that could result from
noise alone. It is considered statistically valid. **But what happens if properly correlated 
noise is used?**
##
The following figure shows the NIE distribution that results from using correlated noise ...
![This NIE plot tells us that event is not statically valid](help_images/NIEwithCorrelatedNoise.png =800x600)
And now we see that the event is just an artifact of chunky noise and its drop even
occurs at the most probable drop of a noise-induced-event.
##
## Conclusion
##
The use of correlated noise is required for proper NIE analysis (it has a much smaller effect 
on edge time error bars as determined by Monte Carlo trials), but ...
##
there is a checkbox on the settings tab that allows the user 
to turn on or off the use of correlated noise. This defaults to *checked* and should
normally be left in that state, but if you want to experiment with the use of white noise
in place of correlated noise,
you can turn it off.
##
## Appendix:
##
For the EY18 example shown above, the lag coefficients extracted from the baseline noise were:
##
```
1.000, 0.572, 0.316, 0.196, 0.131, 0.079, 0.062, 0.055, 0.042, 0.046, 0.045

noise sigma = 0.1198
```
##
The autoregressive model weights that, when applied to white noise,
produce noise with identical lag coefficients to those from the obeservation baseline noise were:
##
```
(c1 ... c10) = 0.5827, -0.0367, 0.0227, 0.0212, -0.0226, 0.0152, 0.0124, -0.0128, 0.0191, 0.0074

innovation variance = 0.670038
```
##
A generated noise point x(n) = c1 * x(n-1) + c2 * x(n-2) + ... + gaussian( sqrt(innovation variance) )
##
This generates noise with a standard deviation of 1.0, a mean of 0.0, and the first 10 lag coefficients
matching those of the baseline noise closely. 
##
This is then multiplied by the noise sigma of the baseline noise (0.1198)
to give the final noise values to be used in the analysis.
##