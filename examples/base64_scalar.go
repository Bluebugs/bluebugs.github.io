// Base64 Decoder - Scalar Version for Browser WASM Demo
// Exports decodeBase64/getInputPtr/getOutputPtr/getOutputLen for JavaScript interop.
package main

import "unsafe"

const (
	maxInput  = 65536
	maxOutput = 65536
)

var (
	inputBuf  [maxInput]byte
	outputBuf [maxOutput]byte
	outputLen int32
)

// decodeSextet converts a single base64 ASCII character to its 6-bit value.
func decodeSextet(ch byte) (byte, bool) {
	switch {
	case 'A' <= ch && ch <= 'Z':
		return ch - 'A', true
	case 'a' <= ch && ch <= 'z':
		return ch - 'a' + 26, true
	case '0' <= ch && ch <= '9':
		return ch - '0' + 52, true
	case ch == '+':
		return 62, true
	case ch == '/':
		return 63, true
	}
	return 0, false
}

// scalarDecode decodes base64-encoded src into dst, returning the number of
// bytes written. Returns -1 on invalid input.
func scalarDecode(dst, src []byte) int {
	if len(src) == 0 {
		return 0
	}
	if len(src)%4 != 0 {
		return -1
	}

	padCount := 0
	if src[len(src)-1] == '=' {
		padCount++
	}
	if len(src) >= 2 && src[len(src)-2] == '=' {
		padCount++
	}

	groups := len(src) / 4
	out := 0

	for g := 0; g < groups; g++ {
		var sextets [4]byte
		for j := 0; j < 4; j++ {
			ch := src[g*4+j]
			if ch == '=' {
				sextets[j] = 0
				continue
			}
			s, ok := decodeSextet(ch)
			if !ok {
				return -1
			}
			sextets[j] = s
		}
		dst[out+0] = (sextets[0] << 2) | (sextets[1] >> 4)
		dst[out+1] = (sextets[1] << 4) | (sextets[2] >> 2)
		dst[out+2] = (sextets[2] << 6) | sextets[3]
		out += 3
	}

	return out - padCount
}

//go:export decodeBase64
func decodeBase64(inputLen int32) int32 {
	n := scalarDecode(outputBuf[:], inputBuf[:inputLen])
	if n < 0 {
		outputLen = 0
		return -1
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
