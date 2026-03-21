# GoPyOTE

## 

## Overview

The primary motivating factors in the development of this app are:

1. ... to fit light curves that include the effects of diffraction, finite star size (with limb darkening), and camera exposure time using the **IOTAdiffraction** app.

2. ... to use a programming language and GUI system that would result in an executable (Windows .exe) with zero dependencies. This makes distribution and installation very simple.

3. ... allow the user to create a parameters file for use by the **IOTAdiffraction** app. It allows for a main body ellipse and an optional satellite body ellipse to be drawn  and postioned on the fundamental plane. Distance to the asteroid(s) along with dX and dY (which gives shadow speed and direction) must be specified. If the star has a known (or suspected) finite size projected at the plane of the asteroid(s), this can be specified along with limb darkening parameters.

4. ... allow an external asteroid shape arrangement (a .png file) to be used to depict an irregular asteroid shape including a possible satellite.

## 

## GoPyOTE versus PyOTE

The rewrite of the Python/QT5 application preserves nearly all of the functionality of **PyOTE**. Here is the current list of changes:

1. Support for the **RunCam** camera is not provided. If you use that camera, you will have to continue using **PyOTE.**

2. Single point validation is greatly simplified (and now allows for multiple points to be specifed) and provides a Monte Carlo demonstration of where that drop appears in a histogram of noise-only drops. Ultimately, the arbiter(s) of single/multiple point validity are currently Dave Herald and Dave Gault. The **GoPyOTE** single/multiple point validation is best used as a  *"don't bother reporting this one"* as opposed to being a validator in its own right.

## 

## Features

- **Light curve visualization** - Interactive plotting with zoom and pan
- **Timing analysis** - Automatic detection of cadence errors and dropped frames
- **Normalization** - Correct for clouds, haze, lighting changes, etc
- **Block integration** - Standard treatment for cameras like the **Watec 910**
- **VizieR** curve export
- **Determination of edge time uncertainties by Monte Carlo trials** using the actual noise from the observation including the important effects of correlated noise.

## 

## Credits

## 

Developed for the occultation astronomy community by **Bob Anderson/IOTA**

 

(bob.anderson.ok@gmail.com)

## 

Chief beta tester:  **Deborah Smith**

 

(debjsmith@me.com)

## 
