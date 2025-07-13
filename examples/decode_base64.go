// Package main demonstrates the SPMD Go decode function from vb64
// Based on: https://github.com/mcy/vb64/blob/main/src/simd.rs#L16-L144
package main

import (
	"lanes"
	"reduce"
)

func Decode(ascii []byte) ([]byte, bool) {
	if len(ascii) == 0 {
		return nil, true // No data to decode
	}
	if len(ascii) % 4 != 0 {
		return nil, false // Base64 requires input length to be a multiple of 4 (could do with padding)
	}

	decoded := make([]byte, 0, len(ascii) * 3 / 4)

	pattern := outputPattern()

	go for _, v := range[4] ascii {
		decodedChunk, valid := decodeChunk(v, pattern)
		if !valid {
			return nil, false // Invalid base64 input
		}

		decoded = append(decoded, decodedChunk...)
	}

	return decoded, true
}


// decode decodes `ascii` as base64. Returns the results of the decoding in the low
// 3/4 of the returned vector, as well as whether decoding completed successfully.
// Direct translation of: pub fn decode<const N: usize>(ascii: Simd<u8, N>) -> (Simd<u8, N>, bool)
func decodeChunk(ascii varying[4] byte, pattern varying[4] uint8) ([]byte, bool) {
	// Perfect hash function: (c >> 4) - (c == '/')
	// This maps the five base64 categories as such:
	//   A..=Z => 4 or 5,
	//   a..=z => 6 or 7,
	//   0..=9 => 3,
	//   +     => 2,
	//   /     => 1,
	
	// let hashes = (ascii >> Simd::splat(4))
	//   + Simd::simd_eq(ascii, Simd::splat(b'/'))
	//     .to_int()
	//     .cast::<u8>();
	hashes := lanes.ShiftRight(ascii, 4) 
	if ascii == '/' {
		hashes += 1
	} else {
		hashes += 0
	}

	// let sextets = ascii + tiled(&[!0, 16, 19, 4, 191, 191, 185, 185]).swizzle_dyn(hashes);
	offsetTable := []byte{255, 16, 19, 4, 191, 191, 185, 185} // !0 = 255
	offsets := lanes.Swizzle(lanes.From(offsetTable), hashes)
	sextets := ascii + offsets
	
	// Range validation using lookup tables
	// const LO_LUT: Simd<u8, 16> = Simd::from_array([
	//   0b10101, 0b10001, 0b10001, 0b10001, 0b10001, 0b10001, 0b10001, 0b10001,
	//   0b10001, 0b10001, 0b10011, 0b11010, 0b11011, 0b11011, 0b11011, 0b11010,
	// ]);
	loLUT := lanes.From([]byte{
		0b10101, 0b10001, 0b10001, 0b10001, 0b10001, 0b10001, 0b10001, 0b10001,
		0b10001, 0b10001, 0b10011, 0b11010, 0b11011, 0b11011, 0b11011, 0b11010,
	})

	// const HI_LUT: Simd<u8, 16> = Simd::from_array([
	//   0b10000, 0b10000, 0b00001, 0b00010, 0b00100, 0b01000, 0b00100, 0b01000,
	//   0b10000, 0b10000, 0b10000, 0b10000, 0b10000, 0b10000, 0b10000, 0b10000,
	// ]);
	hiLUT := lanes.From([]byte{
		0b10000, 0b10000, 0b00001, 0b00010, 0b00100, 0b01000, 0b00100, 0b01000,
		0b10000, 0b10000, 0b10000, 0b10000, 0b10000, 0b10000, 0b10000, 0b10000,
	})

	// let lo = swizzle::<16, N>(LO_LUT, ascii & Simd::splat(0x0f));
	// let hi = swizzle::<16, N>(HI_LUT, ascii >> Simd::splat(4));
	lo := lanes.Swizzle(loLUT, ascii & 0x0f)
	hi := lanes.Swizzle(hiLUT, lanes.ShiftRight(ascii, 4))

	// let valid = (lo & hi).reduce_or() == 0;
	valid := reduce.Or(lo & hi) == 0
	
	// Now we need to shift everything a little bit, since each byte has two high
	// bits it shouldn't that we need to delete. This follows the complex bit
	// manipulation from the Rust implementation:
	
	// let shifted = sextets.cast::<u16>() << tiled(&[2, 4, 6, 8]);
	shiftPattern := lanes.From([]uint16{2, 4, 6, 8})
	shifted := lanes.ShiftLeft(sextets, shiftPattern)

	// let lo = shifted.cast::<u8>();
	// let hi = (shifted >> Simd::splat(8)).cast::<u8>();
	shiftedLo := varying[4] byte(shifted)
	shiftedHi := varying[4] byte(lanes.ShiftRight(shifted, 8))

	// let decoded_chunks = lo | hi.rotate_lanes_left::<1>();
	decodedChunks := shiftedLo | lanes.Rotate(shiftedHi, 1)

	// let output = swizzle!(N; decoded_chunks, array!(N; |i| i + i / 3));
	// The output pattern is skipping every 4th byte, which is why we use `i + i / 3`.
	output := lanes.Swizzle(decodedChunks, pattern)

	return []byte(output), valid
}

func outputPattern() varying[4] uint8 {
	var r varying[4] uint8
	go for i := range[4] {
		r[i] = uint8(i + i/3)
	}
	return r
}

