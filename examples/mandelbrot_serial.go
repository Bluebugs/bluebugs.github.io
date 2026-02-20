// Mandelbrot Set - Serial Version for Browser WASM Demo
// Exports computeMandelbrot/getBufferPtr/getBufferLen for JavaScript interop.
package main

import "unsafe"

const bufSize = 256 * 256

var buf [bufSize]int32

func mandelSerial(cRe, cIm float32, maxIter int32) int32 {
	var zRe, zIm float32 = cRe, cIm
	for i := int32(0); i < maxIter; i++ {
		if zRe*zRe+zIm*zIm > 4.0 {
			return i
		}
		newRe := zRe*zRe - zIm*zIm
		newIm := 2.0 * zRe * zIm
		zRe = cRe + newRe
		zIm = cIm + newIm
	}
	return maxIter
}

//go:export computeMandelbrot
func computeMandelbrot(width, height, maxIter int32) {
	dx := (1.5 - (-2.5)) / float32(width)
	dy := (1.25 - (-1.25)) / float32(height)
	for j := int32(0); j < height; j++ {
		y := -1.25 + float32(j)*dy
		for i := int32(0); i < width; i++ {
			x := -2.5 + float32(i)*dx
			buf[j*width+i] = mandelSerial(x, y, maxIter)
		}
	}
}

//go:export getBufferPtr
func getBufferPtr() int32 {
	return int32(uintptr(unsafe.Pointer(&buf[0])))
}

//go:export getBufferLen
func getBufferLen() int32 { return bufSize }

func main() {}
