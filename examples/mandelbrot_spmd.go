// Mandelbrot Set - SPMD Version for Browser WASM Demo
// Uses go for SIMD loop for ~3x speedup over serial.
// Exports computeMandelbrot/getBufferPtr/getBufferLen for JavaScript interop.
package main

import (
	"lanes"
	"unsafe"
)

const bufSize = 256 * 256

var buf [bufSize]int32

func mandelSPMD(cRe, cIm lanes.Varying[float32], maxIter int) lanes.Varying[int32] {
	var zRe lanes.Varying[float32] = cRe
	var zIm lanes.Varying[float32] = cIm
	var iterations lanes.Varying[int32] = int32(maxIter)

	for iter := range maxIter {
		magSquared := zRe*zRe + zIm*zIm
		diverged := magSquared > 4.0

		if diverged {
			iterations = int32(iter)
			break
		}

		newRe := zRe*zRe - zIm*zIm
		newIm := 2.0 * zRe * zIm
		zRe = cRe + newRe
		zIm = cIm + newIm
	}

	return iterations
}

func mandelbrotSPMD(x0, y0, x1, y1 float32, width, height, maxIter int, output []int32) {
	dx := (x1 - x0) / float32(width)
	dy := (y1 - y0) / float32(height)

	for j := 0; j < height; j++ {
		y := y0 + float32(j)*dy

		go for i := range width {
			x := x0 + lanes.Varying[float32](i)*dx
			iterations := mandelSPMD(x, y, maxIter)
			index := j*width + i
			output[index] = iterations
		}
	}
}

//go:export computeMandelbrot
func computeMandelbrot(width, height, maxIter int32) {
	mandelbrotSPMD(-2.5, -1.25, 1.5, 1.25, int(width), int(height), int(maxIter), buf[:])
}

//go:export getBufferPtr
func getBufferPtr() int32 {
	return int32(uintptr(unsafe.Pointer(&buf[0])))
}

//go:export getBufferLen
func getBufferLen() int32 { return bufSize }

func main() {}
