# Block Integration

## 

Cameras like the commonly used **Watec 910** perform light integration (to increase the effective exposure time) by summing a fixed number of field rate exposures (the number is selected by the observer) in an internal buffer and then outputting the contents of that buffer at the normal field rate. This results in an output where a block of points will have nominally the same value. But, of course, there is some noise introduced during this process.

## 

Block integration is used to find each block of nominally identical values and average them together, resulting in a noise-reduced value that is used for the light curve values.

## 

The timestamp for such a block is taken as the timestamp of the first point in the block.

## 

### To use:

1. Visually inspect the light curve, identify a block, and click on the first point in that block.

2. Click on the last point in that block.

3. Click on the **Block Integrate** button. 

4. If needed, you can start over by clicking the **UnDo** button which operates by re-reading the original file.

## 

## Note:

It is common for there to be a partial block at the beginning and/or end of the data. Those points are simply dropped. Calculations are only performed on complete blocks.

## 
