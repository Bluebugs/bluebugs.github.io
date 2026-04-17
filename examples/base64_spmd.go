// Base64 Decoder - SPMD Version for Browser WASM Demo
// Uses cascading go-for loops (byte→int16→int32) for pmaddubsw/pmaddwd packing.
// Exports decodeBase64/getInputPtr/getOutputPtr/getOutputLen for JavaScript interop.
package main

import (
	"lanes"
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

// Nibble-LUT decode table, indexed by the high nibble of the ASCII character.
//
// '+' (0x2B, sextet 62) and '/' (0x2F, sextet 63) share hi-nibble 2.
// decodeLUT[2] = 16 is correct for '/' (0x2F + 16 = 63).
// '+' needs offset 19, so we add 3 via a varying conditional (SPMD select).
//
// Values computed as (sextet - ASCII) & 0xFF:
//
//	hi=2: 16  (for '/'; '+' corrected via explicit if)
//	hi=3: 4   ('0'-'9')
//	hi=4: 191 ('A'-'O')
//	hi=5: 191 ('P'-'Z')
//	hi=6: 185 ('a'-'o')
//	hi=7: 185 ('p'-'z')
var decodeLUT = [16]byte{
	0, 0, 16, 4, 191, 191, 185, 185,
	0, 0, 0, 0, 0, 0, 0, 0,
}

// decodeSextet converts a single base64 ASCII character to its 6-bit value.
// Used for the scalar tail/padding fallback path.
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

// Scratch buffers for decodeAndPack — pre-allocated to avoid per-call heap
// pressure in the benchmark loop. Sized for max chunk (32 bytes on AVX2).
var scratchSextets [32]byte
var scratchMerged [16]int16
var scratchPacked [8]int32

// decodeAndPack processes one SIMD-register-width chunk of base64 source.
// src must be exactly lanes.Count[byte]() bytes (16 on SSE/WASM, 32 on AVX2).
// Decodes sextets and packs via cascading multiply-add loops that trigger
// pmaddubsw and pmaddwd pattern detection in the compiler.
// Returns number of output bytes written to dst (= len(src) * 3/4).
func decodeAndPack(dst, src []byte) int {
	n := len(src)
	sextets := scratchSextets[:n]
	halfLen := n / 2
	merged := scratchMerged[:halfLen]
	quarterLen := halfLen / 2
	packed := scratchPacked[:quarterLen]

	go for i, ch := range src {
		s := ch + decodeLUT[ch>>4]
		if ch == byte('+') {
			s += 3
		}
		sextets[i] = s
	}

	go for g := range merged {
		merged[g] = int16(sextets[g*2])*64 + int16(sextets[g*2+1])
	}

	go for g := range packed {
		packed[g] = int32(merged[g*2])*4096 + int32(merged[g*2+1])
	}

	go for g := range packed {
		dst[g*3+0] = byte(packed[g] >> 16)
		dst[g*3+1] = byte(packed[g] >> 8)
		dst[g*3+2] = byte(packed[g])
	}

	return quarterLen * 3
}

// spmdDecode decodes base64-encoded src into dst, returning number of bytes
// written. Returns -1 on invalid input (odd group count).
func spmdDecode(dst, src []byte) int {
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
	hotGroups := groups
	if padCount > 0 {
		hotGroups--
	}
	hotBytes := hotGroups * 4

	// Chunk size = byte SIMD width. 16 on WASM/SSE, 32 on AVX2, 4 in scalar mode.
	// The minimum of 4 ensures the cascading byte→int16→int32 loops all produce
	// meaningful work: 4 bytes → 2 int16 → 1 int32 → 3 output bytes. Without it,
	// scalar mode (lanes.Count=1) would give empty int16/int32 loops.
	// In SIMD mode, this ensures each go-for loop runs exactly one iteration,
	// enabling full unrolling and register promotion.
	var bv lanes.Varying[byte]
	chunkSize := max(4, lanes.Count[byte](bv))
	outOffset := 0

	// Process full register-width chunks via SPMD kernel.
	for off := 0; off+chunkSize <= hotBytes; off += chunkSize {
		n := decodeAndPack(dst[outOffset:], src[off:off+chunkSize])
		outOffset += n
	}

	// Handle remaining bytes (less than one full chunk).
	rem := hotBytes % chunkSize
	if rem > 0 && rem%4 == 0 {
		// Pad remainder to chunkSize with 'A' (sextet 0) for safe SIMD execution.
		padded := make([]byte, chunkSize)
		copy(padded, src[hotBytes-rem:hotBytes])
		for i := rem; i < chunkSize; i++ {
			padded[i] = 'A'
		}
		tmpDst := make([]byte, chunkSize)
		n := decodeAndPack(tmpDst, padded)
		validOut := rem * 3 / 4
		copy(dst[outOffset:], tmpDst[:validOut])
		outOffset += validOut
		_ = n
	}

	// Handle trailing padding quartet with scalar fallback.
	if hotGroups < groups {
		tail := src[hotGroups*4:]
		c0, _ := decodeSextet(tail[0])
		c1, _ := decodeSextet(tail[1])
		var c2, c3 byte
		if tail[2] != '=' {
			c2, _ = decodeSextet(tail[2])
		}
		if tail[3] != '=' {
			c3, _ = decodeSextet(tail[3])
		}
		dst[outOffset+0] = (c0 << 2) | (c1 >> 4)
		dst[outOffset+1] = (c1 << 4) | (c2 >> 2)
		dst[outOffset+2] = (c2 << 6) | c3
		outOffset += 3
	}

	return outOffset - padCount
}

//go:export decodeBase64
func decodeBase64(inputLen int32) int32 {
	n := spmdDecode(outputBuf[:], inputBuf[:inputLen])
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
