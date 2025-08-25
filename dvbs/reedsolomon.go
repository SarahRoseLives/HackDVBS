package dvbs

// DVB-S Reed-Solomon RS(204, 188, T=8) Encoder
// Based on the DVB-S standard (ETSI EN 300 421) which uses the CCSDS polynomial.

// RSEncoder holds the precomputed generator polynomial.
type RSEncoder struct {
	generator []byte
}

// NewRSEncoder creates a new encoder for DVB-S.
func NewRSEncoder() *RSEncoder {
	// Generate the generator polynomial g(x) for T=8 (16 parity bytes)
	// g(x) = (x-a^0)(x-a^1)...(x-a^15)
	g := make([]byte, 17)
	g[0] = 1 // g_0 is always 1

	for i := 0; i < 16; i++ {
		alpha_pow := gfExp[i]
		for j := i + 1; j > 0; j-- {
			// g[j] = g[j] * alpha_pow + g[j-1]
			g[j] = gfMul(g[j], alpha_pow) ^ g[j-1]
		}
	}
	// We only need the coefficients from g_1 to g_16 for the feedback implementation.
	return &RSEncoder{generator: g[1:]}
}

// gfMul performs multiplication in the DVB-S specific GF(256) field.
func gfMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return gfExp[int(gfLog[a])+int(gfLog[b])]
}

// Encode takes a 188-byte data packet and returns a 204-byte packet with parity.
// This function now implements a standard, correct systematic Reed-Solomon encoder.
func (e *RSEncoder) Encode(data []byte) []byte {
	if len(data) != 188 {
		return nil // Or handle error appropriately
	}

	out := make([]byte, 204)
	copy(out, data)

	// A 16-byte register for the parity calculation (remainder).
	parityReg := make([]byte, 16)

	// Process each data byte through the feedback shift register.
	for i := 0; i < 188; i++ {
		feedback := data[i] ^ parityReg[0]
		// Shift the register left by one byte.
		copy(parityReg, parityReg[1:])
		parityReg[15] = 0 // Last element is now 0

		// If feedback is non-zero, XOR the register with the generator polynomial.
		if feedback != 0 {
			for j := 0; j < 16; j++ {
				parityReg[j] ^= gfMul(e.generator[j], feedback)
			}
		}
	}

	// The register now holds the correct parity bytes.
	copy(out[188:], parityReg)
	return out
}