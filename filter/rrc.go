package filter

import (
	"math"
)

type FIRFilter struct {
	Taps           []float64
	State          []complex128
	UpsampleFactor int
}

func NewRRCFilter(symbolRate, sampleRate, rollOff float64, numTaps int) *FIRFilter {
	taps := make([]float64, numTaps)
	Ts := 1.0 / symbolRate
	for i := 0; i < numTaps; i++ {
		t := float64(i) - float64(numTaps-1)/2.0
		t /= sampleRate
		if t == 0 {
			taps[i] = (1.0 / Ts) * (1.0 - rollOff + 4.0*rollOff/math.Pi)
		} else if math.Abs(math.Abs(4.0*rollOff*t/Ts)-1.0) < 1e-9 { // Avoid division by zero
			val := (rollOff / (Ts * math.Sqrt2)) * ((1+2/math.Pi)*math.Sin(math.Pi/(4.0*rollOff)) + (1-2/math.Pi)*math.Cos(math.Pi/(4.0*rollOff)))
			taps[i] = val
		} else {
			num := (1.0 / Ts) * (math.Sin(math.Pi*t/Ts*(1-rollOff)) + 4*rollOff*t/Ts*math.Cos(math.Pi*t/Ts*(1+rollOff)))
			den := math.Pi * t / Ts * (1 - (4*rollOff*t/Ts)*(4*rollOff*t/Ts))
			taps[i] = num / den
		}
	}
	var gain float64
	for i := 0; i < len(taps); i += int(sampleRate / symbolRate) {
		gain += taps[i]
	}
	for i := range taps {
		taps[i] /= gain
	}
	return &FIRFilter{
		Taps:           taps,
		State:          make([]complex128, (numTaps-1)/int(sampleRate/symbolRate)+1),
		UpsampleFactor: int(sampleRate / symbolRate),
	}
}

func (f *FIRFilter) Process(symbols []complex128) []complex128 {
	outputSamples := make([]complex128, len(symbols)*f.UpsampleFactor)
	for i, symbol := range symbols {
		copy(f.State[1:], f.State)
		f.State[0] = symbol
		for j := 0; j < f.UpsampleFactor; j++ {
			var out complex128
			for k := 0; k < len(f.State); k++ {
				tapIndex := k*f.UpsampleFactor + j
				if tapIndex < len(f.Taps) {
					// CORRECTED: Access taps in forward order for convolution.
					out += f.State[k] * complex(f.Taps[tapIndex], 0)
				}
			}
			outputSamples[i*f.UpsampleFactor+j] = out
		}
	}
	return outputSamples
}