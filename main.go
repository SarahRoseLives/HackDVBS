package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"

	"github.com/samuel/go-hackrf/hackrf"
	"hackdvbs/consts"
	"hackdvbs/dvbs"
	"hackdvbs/filter"
	"hackdvbs/utils"
)

const (
	// Create a buffer of 16 million samples, which is 2.10 seconds of data at 8 Msps.
	numSamples = 16 * 1024 * 1024
)

func main() {
	freq := flag.Float64("freq", 1280.0, "Transmit frequency in MHz")
	gain := flag.Int("gain", 30, "TX VGA gain (0-47)")
	flag.Parse()

	// --- PHASE 1: PRE-COMPUTATION ---
	log.Println("--- Preparing to transmit from test_stream.ts ---")

	precomputedSamples := make([]complex128, numSamples)
	iqChannel := make(chan complex128, 1024*1024)

	rrcFilter := filter.NewRRCFilter(consts.SymbolRate, consts.HackRFSampleRate, consts.RollOffFactor, consts.RRCFilterTaps)
	dvbsEncoder := dvbs.NewDVBSEncoder()

	// **REMOVED FFmpeg**: Open the local TS file directly.
	tsFile, err := os.Open("test_stream.ts")
	if err != nil {
		log.Fatalf("Failed to open test_stream.ts: %v", err)
	}
	defer tsFile.Close()

	// Start the DVB-S encoding process, reading from the file.
	go dvbs.StreamToIQ(tsFile, iqChannel, dvbsEncoder, rrcFilter)

	log.Printf("Generating %d I/Q samples (%.2f seconds of signal)...", numSamples, float64(numSamples)/consts.HackRFSampleRate)
	for i := 0; i < numSamples; i++ {
		sample, ok := <-iqChannel
		if !ok {
			// This is now expected if test_stream.ts is shorter than the buffer size.
			log.Println("Finished reading test_stream.ts. The pre-computed buffer might not be full.")
			// Fill the rest of the buffer with silence (zeros)
			for j := i; j < numSamples; j++ {
				precomputedSamples[j] = complex(0, 0)
			}
			break // Exit the loop
		}
		precomputedSamples[i] = sample
	}
	log.Println("Sample generation complete.")

	// --- PHASE 2: TRANSMISSION ---
	log.Println("Starting transmission phase...")

	if err := hackrf.Init(); err != nil {
		log.Fatalf("hackrf.Init() failed: %v", err)
	}
	defer hackrf.Exit()

	dev, err := hackrf.Open()
	if err != nil {
		log.Fatalf("hackrf.Open() failed: %v", err)
	}
	defer dev.Close()

	dev.SetFreq(uint64(*freq * 1_000_000))
	dev.SetSampleRate(consts.HackRFSampleRate)
	dev.SetTXVGAGain(*gain)
	dev.SetAmpEnable(true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var sampleIndex int
	err = dev.StartTX(func(buf []byte) error {
		select {
		case <-ctx.Done():
			return errors.New("transfer cancelled")
		default:
		}

		const digitalGain = 110.0
		samplesToWrite := len(buf) / 2
		for i := 0; i < samplesToWrite; i++ {
			sample := precomputedSamples[sampleIndex]
			i_sample := int8(real(sample) * digitalGain)
			q_sample := int8(imag(sample) * digitalGain)
			buf[i*2] = byte(i_sample)
			buf[i*2+1] = byte(q_sample)

			sampleIndex = (sampleIndex + 1) % numSamples
		}
		return nil
	})

	if err != nil {
		if err.Error() != "transfer cancelled" {
			log.Fatalf("StartTX failed: %v", err)
		}
	}
	log.Println("Transmission is live and looping. Press Ctrl+C to stop.")

	utils.WaitForSignal()
	log.Println("Stopping transmission...")
	cancel()

	dev.StopTX()
	log.Println("Transmission stopped.")
}