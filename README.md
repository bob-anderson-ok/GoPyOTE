# GoPyOTE

This is a re-write of PyOTE in Golang using Fyne V2 for the GUI elements.

PyOTE (**Py**thon **O**ccultation **T**iming **E**xtractor) was written in Python 3.8 using QT5 for the GUI. That
app is still available and can be found [here](https://github.com/bob-anderson-ok/py-ote).

The re-write in Golang uses Fyne V2 for the GUI elements and was undertaken for two reasons:
1. To fit a comprehensive theoretical light curve which includes the effects of:
   - Diffraction
   - Finite star size
   - Limb darkening of a finite star
   - Camera exposure time
2. To produce an app that can be distributed as a zip file with two .exe files and a few supporting *starter* files.
3. An installation that has no dependencies.


**GoPyOTE** does not support the RunCam camera. For that camera, PyOTE will have to be used.

See **Claude.md** for detailed information about the re-write.