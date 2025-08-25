package consts

import "math"

// Correct DVB-S QPSK Gray mapping
var QPSKSymbolMap = map[byte]complex128{
	0: complex(1/math.Sqrt2, 1/math.Sqrt2),  // 00
	1: complex(1/math.Sqrt2, -1/math.Sqrt2), // 01
	2: complex(-1/math.Sqrt2, 1/math.Sqrt2), // 10
	3: complex(-1/math.Sqrt2, -1/math.Sqrt2), // 11
}