// Base64 Decoder - Go stdlib Version for Browser WASM Demo
// Uses encoding/base64.StdEncoding.Decode for a real-world baseline.
// Exports decodeBase64/getInputPtr/getOutputPtr/getOutputLen for JavaScript interop.
package main

import (
	"encoding/base64"
	"unsafe"
)

const (
	maxInput  = 65536
	maxOutput = 65536
)

var (
	inputBuf  [maxInput]byte
	outputBuf [maxOutput]byte
	outputLen int32
)

//go:export decodeBase64
func decodeBase64(inputLen int32) int32 {
	n, err := base64.StdEncoding.Decode(outputBuf[:], inputBuf[:inputLen])
	if err != nil {
		outputLen = 0
		return 0
	}
	outputLen = int32(n)
	return outputLen
}

//go:export getInputPtr
func getInputPtr() int32 {
	return int32(uintptr(unsafe.Pointer(&inputBuf[0])))
}

//go:export getOutputPtr
func getOutputPtr() int32 {
	return int32(uintptr(unsafe.Pointer(&outputBuf[0])))
}

//go:export getOutputLen
func getOutputLen() int32 { return outputLen }

func main() {}
