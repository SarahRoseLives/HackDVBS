package dvbs

// DVB-S Reed-Solomon RS(204, 188, T=8) Encoder
// Based on the DVB-S standard (ETSI EN 300 421) which uses the CCSDS polynomial.

// RSEncoder holds the precomputed generator polynomial.
type RSEncoder struct {
	generator []byte
}

// NewRSEncoder creates a new encoder for DVB-S.
func NewRSEncoder() *RSEncoder {
	// Use the exact hardcoded generator polynomial from the SDRangel source
	// to ensure a perfect "bug-for-bug" compatible match.
	generatorPoly := []byte{
		59, 13, 104, 189, 68, 209, 30, 8, 163, 65, 41, 229, 98, 50, 36, 59,
	}
	return &RSEncoder{generator: generatorPoly}
}

// gfMul performs multiplication in the DVB-S specific GF(256) field.
func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[int(gfLog[a])+int(gfLog[b])]
}

// Encode takes a 188-byte data packet and returns a 204-byte packet with parity.
// **FIX**: This function now perfectly replicates the non-standard polynomial
// division algorithm used in SDRangel's DVB-S transmitter.
func (e *RSEncoder) Encode(data []byte) []byte {
	if len(data) != 188 {
		return nil // Or handle error appropriately
	}

	// Create a temporary buffer of 204 bytes to work in, mimicking the C++ implementation.
	// The original C++ code uses a 239-byte buffer but only operates on the first 204 bytes.
	tmp := make([]byte, 204)
	copy(tmp, data) // Copy the 188 bytes of data, the rest is already zero.

	// This loop performs polynomial division to calculate the remainder (parity bytes).
	for i := 0; i < 188; i++ {
		coef := tmp[i]
		if coef != 0 {
			for j := 0; j < 16; j++ {
				tmp[i+j+1] ^= gfMul(e.generator[j], coef)
			}
		}
	}

	// The last 16 bytes of tmp now contain the correct parity bytes.
	// Append them to the original data to form the final 204-byte packet.
	out := make([]byte, 204)
	copy(out, data)
	copy(out[188:], tmp[188:])

	return out
}