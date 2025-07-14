// Package main demonstrates SPMD IPv4 address parsing using 16-lane processing
// Based on: https://github.com/WojciechMula/toys/blob/master/parseip4/sse_v7.cpp.inl
package main

import (
	"fmt"
	"math/bits"
	"reduce"
)

type parseAddrError struct {
	in  string
	msg string
	at  string
}

func (e parseAddrError) Error() string {
	if e.at != "" {
		return fmt.Sprintf("parse %q: %s (at %q)", e.in, e.msg, e.at)
	}
	return fmt.Sprintf("parse %q: %s", e.in, e.msg)
}

// parseIPv4 processes IPv4 addresses using 16-lane SPMD similar to Wojciech's SSE approach
func parseIPv4(s string) ([4]byte, error) {
	if len(s) < 7 || len(s) > 15 {
		return [4]byte{}, parseAddrError{in: s, msg: "IPv4 address string too short or too long"}
	}

	// Pad string to 16 bytes with null terminators (like SSE register)
	input := [16]byte{}
	copy(input[:], s)

	// Process all 16 lanes in parallel
	var dotMaskTotal varying[16] uint8
	var dotMask varying[16] bool
	var digitMask varying[16] bool
	var validChars varying[16] bool

	go for i, c := range[16] input {
		dotMask[i] = c == '.'
		if dotMask[i] {
			dotMaskTotal[i] = 1
		}
		digitMask[i] = (c >= '0' && c <= '9')
		
		// Valid if dot, digit, or null (padding)
		validChars[i] = dotMask[i] || digitMask[i] || c == 0
	}

	// Check character validity
	if !reduce.All(validChars) {
		return [4]byte{}, parseAddrError{in: s, at: reduce.FindFirstSet(validChars), msg: "unexpected character"}
	}

	dotCount := reduce.Sum(dotMaskTotal)
	if dotCount != 3 {
		return [4]byte{}, parseAddrError{in: s, msg: "invalid dot count"}
	}

	// Create dot position bitmask from lanes (similar to _mm_movemask_epi8)
	dotPositionMask := reduce.Mask(dotMask)

	// Extract dot positions (similar to Wojciech's pattern matching)
	var dotPositions [3]int
	mask := dotPositionMask
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

	// Process all four fields in parallel using 4-lane processing
	var ip [4]byte
	var errors [4]parseAddrError
	var hasError varying[4] bool

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

		// Validation (following Wojciech's approach)
		if hasLeadingZero {
			errors[field] = parseAddrError{in: s, msg: "IPv4 field has octet with leading zero"}
			hasError[field] = true
		} else if value > 255 {
			errors[field] = parseAddrError{in: s, msg: "IPv4 field has value >255"}
			hasError[field] = true
		} else {
			ip[field] = uint8(value)
		}
	}

	if reduce.Any(hasError) {
		return [4]byte{}, errors[reduce.FindFirstSet(hasError)]
	}

	return ip, nil
}
