// Package main demonstrates SPMD IPv4 address parsing using parallel processing
// Based on: https://github.com/WojciechMula/toys/blob/master/parseip4/sse_v7.cpp.inl
package main

import (
	"fmt"
	"lanes"
	"math/bits"
	"reduce"
)

type parseAddrError struct {
	in  string
	at  int
	msg string
}

func (e parseAddrError) Error() string {
	if e.at >= 0 {
		return fmt.Sprintf("parse %s at position %d: %s", e.in, e.at, e.msg)
	}
	return fmt.Sprintf("parse %s: %s", e.in, e.msg)
}

// parseIPv4 processes IPv4 addresses using SPMD similar to Wojciech's SSE approach
func parseIPv4(s string) ([4]byte, error) {
	if len(s) < 7 || len(s) > 15 {
		return [4]byte{}, parseAddrError{in: s, msg: "IPv4 address string too short or too long"}
	}

	// Pad string to 16 bytes with null terminators (like SSE register)
	input := [16]byte{}
	copy(input[:], s)

	// Process all bytes in parallel
	var dotMask [16]bool
	var dotMaskTotal lanes.Varying[uint32]

	var loop int
	go for i, c := range input {
		dotMask[i] = c == '.'
		if dotMask[i] {
			dotMaskTotal++
		}
		digitMask := (c >= '0' && c <= '9')

		// Valid if dot, digit, or null (padding)
		validChars := dotMask[i] || digitMask || c == 0

		// Check character validity with precise error location
		if !reduce.All(validChars) {
			return [4]byte{}, parseAddrError{in: s, at: reduce.FindFirstSet(!validChars) + loop, msg: "unexpected character"}
		}
		loop += lanes.Count(c)
	}

	// Count dots using reduction
	dotCount := reduce.Add(dotMaskTotal)
	if dotCount != 3 {
		return [4]byte{}, parseAddrError{in: s, msg: "invalid dot count"}
	}

	// Create dot position bitmask from lanes (similar to _mm_movemask_epi8)
	var mask uint16
	loop = 0
	go for _, isDot := range dotMask {
		mask |= uint16(reduce.Mask(isDot)) << loop
		loop += lanes.Count(isDot)
	}

	// Extract dot positions (similar to Wojciech's pattern matching)
	var dotPositions [3]int
	for i := 0; i < 3; i++ {
		pos := bits.TrailingZeros16(mask)
		dotPositions[i] = pos
		mask &= mask - 1
	}

	// Define field boundaries
	starts := [4]int{0, dotPositions[0], dotPositions[1], dotPositions[2]}
	ends := [4]int{dotPositions[0], dotPositions[1], dotPositions[2], len(s)}

	// Validate field lengths (following Wojciech's max digit approach)
	go for i, start := range starts {
		end := ends[i]
		if i > 0 {
			start++ // Skip the dot
		}
		fieldLen := end - start
		if reduce.Any(fieldLen < 1 || fieldLen > 3) {
			return [4]byte{}, parseAddrError{in: s, msg: "invalid field length"}
		}
	}

	// Process all four fields in parallel
	var ip [4]byte

	go for field, start := range starts {
		end := ends[field]

		if field > 0 {
			start++ // Skip the dot
		}

		fieldLen := end - start
		var value int
		var hasLeadingZero bool

		// Convert field using parallel digit processing
		// This mirrors Wojciech's SSE_CONVERT_MAX1/2/3 macros
		switch fieldLen {
		case 1:
			// Single digit: direct conversion
			value = int(s[start] - '0')
		case 2:
			// Two digits: d1*10 + d0
			d1 := int(s[start] - '0')
			d0 := int(s[start+1] - '0')
			value = d1*10 + d0
			hasLeadingZero = (d1 == 0)
		case 3:
			// Three digits: d2*100 + d1*10 + d0
			d2 := int(s[start] - '0')
			d1 := int(s[start+1] - '0')
			d0 := int(s[start+2] - '0')
			value = d2*100 + d1*10 + d0
			hasLeadingZero = (d2 == 0)
		}

		// Validation: check each error condition across all lanes
		if reduce.Any(hasLeadingZero) {
			return [4]byte{}, parseAddrError{in: s, msg: "IPv4 field has octet with leading zero"}
		}
		if reduce.Any(value > 255) {
			return [4]byte{}, parseAddrError{in: s, msg: "IPv4 field has value >255"}
		}
		ip[field] = uint8(value)
	}

	return ip, nil
}
