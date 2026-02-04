package consts

import "math"

// Non-Standard DVB-S QPSK Gray mapping to match the SDRangel implementation.
// The mappings for bits 01 and 10 are swapped compared to the ETSI standard.
var QPSKSymbolMap = map[byte]complex128{
	0: complex(1/math.Sqrt2, 1/math.Sqrt2),   // bits 00 -> ( 1,  1)
	1: complex(1/math.Sqrt2, -1/math.Sqrt2),  // bits 01 -> ( 1, -1) [SWAPPED]
	2: complex(-1/math.Sqrt2, 1/math.Sqrt2), // bits 10 -> (-1,  1) [SWAPPED]
	3: complex(-1/math.Sqrt2, -1/math.Sqrt2), // bits 11 -> (-1, -1)
}

// Fast QPSK lookup - array is faster than map
var QPSKFast = [4]complex64{
	complex64(complex(1/math.Sqrt2, 1/math.Sqrt2)),
	complex64(complex(1/math.Sqrt2, -1/math.Sqrt2)),
	complex64(complex(-1/math.Sqrt2, 1/math.Sqrt2)),
	complex64(complex(-1/math.Sqrt2, -1/math.Sqrt2)),
}