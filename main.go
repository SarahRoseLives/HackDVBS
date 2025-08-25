package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os/exec"

	"github.com/samuel/go-hackrf/hackrf"
	"hackdvbs/consts"
	"hackdvbs/dvbs"
	"hackdvbs/filter"
	"hackdvbs/utils"
)

const (
	// Create a buffer of 16 million samples, which is 2 seconds of data at 8 Msps.
	numSamples = 16 * 1024 * 1024
)

func main() {
	freq := flag.Float64("freq", 1280.0, "Transmit frequency in MHz")
	gain := flag.Int("gain", 30, "TX VGA gain (0-47)")
	flag.Parse()

	// --- PHASE 1: PRE-COMPUTATION ---
	log.Println("Starting signal pre-computation phase...")

	precomputedSamples := make([]complex128, numSamples)
	iqChannel := make(chan complex128, 1024*1024)

	rrcFilter := filter.NewRRCFilter(consts.SymbolRate, consts.HackRFSampleRate, consts.RollOffFactor, consts.RRCFilterTaps)
	dvbsEncoder := dvbs.NewDVBSEncoder()

	// CORRECTED: Increased -muxrate to 2000k to provide more headroom and prevent packet drops.
	ffmpegCmd := exec.Command("ffmpeg",
		"-f", "lavfi", "-i", "testsrc=size=720x576:rate=25:decimals=2",
		"-f", "lavfi", "-i", "anullsrc",
		"-vcodec", "mpeg2video",
		"-b:v", "1500k", "-maxrate", "1500k", "-bufsize", "3000k",
		"-acodec", "mp2", "-b:a", "128k",
		"-f", "mpegts",
		"-muxrate", "2000k", // Reverted to 2000k for more overhead
		"-max_delay", "500000",
		"-pat_period", "0.1",
		"-pcr_period", "20",
		"-",
	)

	ffmpegStdout, _ := ffmpegCmd.StdoutPipe()
	ffmpegStderr, _ := ffmpegCmd.StderrPipe()
	if err := ffmpegCmd.Start(); err != nil {
		log.Fatalf("Failed to start FFmpeg: %v", err)
	}

	go dvbs.StreamToIQ(ffmpegStdout, iqChannel, dvbsEncoder, rrcFilter)
	go utils.LogFFmpeg(ffmpegStderr)

	log.Printf("Generating %d I/Q samples (%.2f seconds of signal)...", numSamples, float64(numSamples)/consts.HackRFSampleRate)
	for i := 0; i < numSamples; i++ {
		sample, ok := <-iqChannel
		if !ok {
			log.Fatalf("IQ channel closed before buffer was full. FFmpeg may have exited prematurely.")
		}
		precomputedSamples[i] = sample
	}
	log.Println("Sample generation complete.")

	if err := ffmpegCmd.Process.Kill(); err != nil {
		log.Printf("Failed to kill FFmpeg process: %v", err)
	}

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