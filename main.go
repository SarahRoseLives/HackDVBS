package main

import (
    "context"
    "errors"
    "flag"
    "log"
    "os/exec"
    "strconv"

    "github.com/samuel/go-hackrf/hackrf"
    "hackdvbs/consts"
    "hackdvbs/dvbs"
    "hackdvbs/filter"
    "hackdvbs/utils"
)

const (
    // Buffer size for streaming mode - smaller buffer for lower latency
    streamBufferSize = 2 * 1024 * 1024 // 0.25 seconds at 8 Msps
)

func main() {
    freq := flag.Float64("freq", 1280.0, "Transmit frequency in MHz")
    gain := flag.Int("gain", 30, "TX VGA gain (0-47)")
    device := flag.String("device", "/dev/video0", "Video device (Linux) or device index (e.g., '0' for Windows/Mac)")
    videoSize := flag.String("size", "640x480", "Video resolution (e.g., 640x480, 1280x720)")
    videoBitrate := flag.String("vbitrate", "1M", "Video bitrate (e.g., 500k, 1M, 2M)")
    audioBitrate := flag.String("abitrate", "128k", "Audio bitrate (e.g., 64k, 128k)")
    fps := flag.Int("fps", 25, "Frames per second")
    flag.Parse()

    log.Println("--- Starting DVB-S Webcam Transmitter ---")
    log.Printf("Frequency: %.2f MHz, Gain: %d dB", *freq, *gain)
    log.Printf("Video: %s @ %d fps, bitrate: %s", *videoSize, *fps, *videoBitrate)

    // Start FFmpeg to capture webcam and encode to MPEG-TS
    ffmpegCmd := buildFFmpegCommand(*device, *videoSize, *fps, *videoBitrate, *audioBitrate)

    ffmpegStdout, err := ffmpegCmd.StdoutPipe()
    if err != nil {
        log.Fatalf("Failed to get FFmpeg stdout pipe: %v", err)
    }

    ffmpegStderr, err := ffmpegCmd.StderrPipe()
    if err != nil {
        log.Fatalf("Failed to get FFmpeg stderr pipe: %v", err)
    }

    if err := ffmpegCmd.Start(); err != nil {
        log.Fatalf("Failed to start FFmpeg: %v", err)
    }
    defer ffmpegCmd.Process.Kill()

    // Log FFmpeg output in background
    go utils.LogFFmpeg(ffmpegStderr)

    // Initialize HackRF
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

    // Create DVB-S encoder and filter
    rrcFilter := filter.NewRRCFilter(consts.SymbolRate, consts.HackRFSampleRate, consts.RollOffFactor, consts.RRCFilterTaps)
    dvbsEncoder := dvbs.NewDVBSEncoder()

    // Create I/Q sample buffer and channel
    iqChannel := make(chan complex128, 512*1024)
    sampleBuffer := make([]complex128, streamBufferSize)
    bufferReadPos := 0
    bufferWritePos := 0

    // Start the DVB-S encoding goroutine
    go dvbs.StreamToIQ(ffmpegStdout, iqChannel, dvbsEncoder, rrcFilter)

    // Pre-fill buffer
    log.Println("Pre-filling buffer...")
    for i := 0; i < streamBufferSize; i++ {
        sample, ok := <-iqChannel
        if !ok {
            log.Fatal("Stream ended before buffer was filled")
        }
        sampleBuffer[i] = sample
    }
    bufferWritePos = 0
    log.Println("Buffer filled, starting transmission...")

    // Background goroutine to continuously fill the buffer
    go func() {
        for sample := range iqChannel {
            sampleBuffer[bufferWritePos] = sample
            bufferWritePos = (bufferWritePos + 1) % streamBufferSize
        }
    }()

    // Start transmission
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    const digitalGain = 110.0

    err = dev.StartTX(func(buf []byte) error {
        select {
        case <-ctx.Done():
            return errors.New("transfer cancelled")
        default:
        }

        samplesToWrite := len(buf) / 2
        for i := 0; i < samplesToWrite; i++ {
            sample := sampleBuffer[bufferReadPos]
            i_sample := int8(real(sample) * digitalGain)
            q_sample := int8(imag(sample) * digitalGain)
            buf[i*2] = byte(i_sample)
            buf[i*2+1] = byte(q_sample)

            bufferReadPos = (bufferReadPos + 1) % streamBufferSize
        }
        return nil
    })

    if err != nil {
        if err.Error() != "transfer cancelled" {
            log.Fatalf("StartTX failed: %v", err)
        }
    }

    log.Println("Transmission is live. Press Ctrl+C to stop.")
    utils.WaitForSignal()

    log.Println("Stopping transmission...")
    cancel()
    dev.StopTX()
    ffmpegCmd.Process.Kill()
    log.Println("Transmission stopped.")
}

func buildFFmpegCommand(device, videoSize string, fps int, videoBitrate, audioBitrate string) *exec.Cmd {
    // Detect platform and build appropriate FFmpeg command
    var args []string

    // Check if device looks like a path (Linux) or index (Windows/Mac)
    if len(device) > 0 && device[0] == '/' {
        // Linux - use v4l2
        args = []string{
            "-f", "v4l2",
            "-input_format", "mjpeg",
            "-video_size", videoSize,
            "-framerate", strconv.Itoa(fps),
            "-i", device,
            "-f", "alsa",
            "-i", "default",
        }
    } else {
        // Windows/Mac - try different input formats
        // For Windows: use dshow
        // For Mac: use avfoundation
        // This is a simplified version - you may need to adjust based on your OS
        args = []string{
            "-f", "v4l2", // Change to "dshow" for Windows or "avfoundation" for Mac
            "-video_size", videoSize,
            "-framerate", strconv.Itoa(fps),
            "-i", device,
            "-f", "alsa", // Change to "dshow" for Windows or "avfoundation" for Mac
            "-i", "default",
        }
    }

    // Common encoding parameters
    args = append(args,
        "-c:v", "mpeg2video",
        "-b:v", videoBitrate,
        "-maxrate", videoBitrate,
        "-bufsize", "2M",
        "-g", "25", // GOP size
        "-c:a", "mp2",
        "-b:a", audioBitrate,
        "-ar", "48000",
        "-ac", "2",
        "-f", "mpegts",
        "-muxrate", "2M",
        "-",
    )

    return exec.Command("ffmpeg", args...)
}