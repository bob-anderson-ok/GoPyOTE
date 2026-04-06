# Getting started – initial installation

1. Go to the **Releases** section of this repository https://github.com/bob-anderson-ok/GoPyOTE/releases.

2. Download the asset there titled ```Download-for-initial-installation-only.zip```

3. Unzip that download to a convenient (easy-to-find) location in your file system. Inside the zipfile is a folder named **GoPyOTE-HOME** which contains the **GoPyOTE.exe** and other files that are needed by the application.

4. Run **GoPyOTE.exe** 

5. The application will start up with a suggestion to set the directory path to the folder where you keep each observation folder. This is an aid to navigation only, not a critical item and only happens the first time the app is run.

6. In the lower left corner of the main window is a button titled **Check for updates**. Click this to get a list of available versions. If that dialog says that you are running the current (latest) version, there is nothing to do. If there are later versions available than the one included in the zipfile, install the latest version.

7. To access helpful videos for assistance in using the program, click the ```Help Topics``` tab at the top left of the main window, then click the menu item titled ```Video library```. This connects to a GitHub repository called GoPyOTE-VIDEOS and displays a list of available videos available for downloading. If you select one and download it, it will be automatically opened in the default viewer that your system uses for .mp4 videos.

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
