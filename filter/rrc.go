package filter

import "math"

type FIRFilter struct {
	Taps           []float32
	State          []complex64
	UpsampleFactor int
}

func NewRRCFilter(symbolRate, sampleRate, rollOff float64, numTaps int) *FIRFilter {
	taps := make([]float32, numTaps)
	Ts := 1.0 / symbolRate
	
	var gain float64
	for i := 0; i < numTaps; i++ {
		t := float64(i) - float64(numTaps-1)/2.0
		t /= sampleRate
		var tapVal float64
		if t == 0 {
			tapVal = (1.0 / Ts) * (1.0 - rollOff + 4.0*rollOff/math.Pi)
		} else if math.Abs(math.Abs(4.0*rollOff*t/Ts)-1.0) < 1e-9 {
			tapVal = (rollOff / (Ts * math.Sqrt2)) * ((1+2/math.Pi)*math.Sin(math.Pi/(4.0*rollOff)) + (1-2/math.Pi)*math.Cos(math.Pi/(4.0*rollOff)))
		} else {
			num := (1.0 / Ts) * (math.Sin(math.Pi*t/Ts*(1-rollOff)) + 4*rollOff*t/Ts*math.Cos(math.Pi*t/Ts*(1+rollOff)))
			den := math.Pi * t / Ts * (1 - (4*rollOff*t/Ts)*(4*rollOff*t/Ts))
			tapVal = num / den
		}
		taps[i] = float32(tapVal)
		if i%int(sampleRate/symbolRate) == 0 {
			gain += tapVal
		}
	}
	for i := range taps {
		taps[i] /= float32(gain)
	}
	return &FIRFilter{
		Taps:           taps,
		State:          make([]complex64, (numTaps-1)/int(sampleRate/symbolRate)+1),
		UpsampleFactor: int(sampleRate / symbolRate),
	}
}

func (f *FIRFilter) Process(symbols []complex64) []complex64 {
	outputLen := len(symbols) * f.UpsampleFactor
	outputSamples := make([]complex64, outputLen)
	
	stateLen := len(f.State)
	tapsLen := len(f.Taps)
	upFactor := f.UpsampleFactor
	
	for symIdx := 0; symIdx < len(symbols); symIdx++ {
		// Shift state
		for k := stateLen - 1; k > 0; k-- {
			f.State[k] = f.State[k-1]
		}
		f.State[0] = symbols[symIdx]
		
		baseOut := symIdx * upFactor
		
		// Generate upsampled outputs
		for j := 0; j < upFactor; j++ {
			var outR, outI float32
			
			for k := 0; k < stateLen; k++ {
				tapIndex := k*upFactor + j
				if tapIndex < tapsLen {
					tap := f.Taps[tapIndex]
					outR += real(f.State[k]) * tap
					outI += imag(f.State[k]) * tap
				}
			}
			outputSamples[baseOut+j] = complex(outR, outI)
		}
	}
	return outputSamples
}