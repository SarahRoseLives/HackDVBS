# Working DVB-S Webcam Transmitter

## The Solution: leandvbtx Pipeline

After extensive optimization, we found that the Go-based encoder can't keep up with real-time requirements (buffer underflows). The **working solution** uses the proven `leandvbtx` encoder from the leansdr project.

### Performance Comparison

**Go Implementation:**
- 500k+ underflows per minute
- Pixelated/stuttering video
- Complex64/float32 optimizations not enough

**leandvbtx Pipeline:**
- 0 underflows
- Perfect signal quality (90% strength, 85%+ quality)
- Smooth real-time video playback

## Quick Start

```bash
# Easy way - use the wrapper script
./transmit_leandvbtx.sh [frequency_hz] [gain] [video_device] [audio_device]

# Example with defaults (1250 MHz, 47dB gain)
./transmit_leandvbtx.sh

# Custom frequency
./transmit_leandvbtx.sh 1280000000 47 /dev/video0 default
```

## Manual Command

```bash
ffmpeg -thread_queue_size 512 -f v4l2 -video_size 640x480 -framerate 30 -i /dev/video0 \
  -thread_queue_size 512 -f alsa -i default \
  -r 30 -c:v mpeg2video -pix_fmt yuv420p -b:v 700k -maxrate 700k -bufsize 1400k \
  -g 10 -bf 0 -c:a mp2 -b:a 128k -ar 44100 \
  -f mpegts -muxrate 1M -pcr_period 20 - | \
/tmp/leansdr/src/apps/leandvbtx --s16 --cr 1/2 -f 2 | \
/tmp/s16_to_s8 | \
hackrf_transfer -t - -f 1250000000 -s 2000000 -x 47 -a 1
```

## Key Settings

### DVB-S Parameters
- **Symbol Rate:** 1 Msps
- **Modulation:** QPSK
- **FEC:** 1/2 (rate 1/2 convolutional)
- **Sample Rate:** 2 Msps (HackRF minimum)
- **Roll-off:** 0.35

### Video Encoding
- **Bitrate:** 700k video + 128k audio = 828 kbps total
- **Muxrate:** 1M (fits within DVB-S channel capacity ~920 kbps)
- **Format:** MPEG-2 video, yuv420p, 640x480 @ 30fps
- **GOP:** 10 frames, no B-frames
- **Buffer:** 1400k (2× video bitrate)

### Why These Settings?

DVB-S channel capacity calculation:
```
Max TS bitrate = SymbolRate × ModulationBits × FEC_Rate × RS_Overhead
                = 1 Msps × 1 bit (QPSK) × 0.5 (FEC 1/2) × (188/204)
                = ~920 kbps
```

Our encoding: 700k + 128k = 828 kbps (fits with overhead)

### Framerate Issue

The webcam outputs 30 fps natively. Encoding at 25 fps caused slow playback because FFmpeg was dropping frames. Setting `-r 30` forces proper 30 fps output.

## Building leandvbtx

```bash
cd /tmp
git clone https://github.com/pabr/leansdr.git
cd leansdr/src/apps
make leandvbtx
```

## Building s16_to_s8 Converter

HackRF uses 8-bit I/Q samples, but leandvbtx outputs 16-bit. Simple converter:

```c
#include <stdio.h>
#include <stdint.h>
#include <unistd.h>

int main() {
    int16_t buf16[8192];
    int8_t buf8[8192];
    ssize_t n;
    
    while ((n = read(0, buf16, sizeof(buf16))) > 0) {
        int samples = n / 2;
        for (int i = 0; i < samples; i++) {
            buf8[i] = buf16[i] >> 8;  // Take high byte
        }
        write(1, buf8, samples);
    }
    return 0;
}
```

Compile: `gcc -O3 s16_to_s8.c -o s16_to_s8`

## Troubleshooting

**Slow/laggy video:**
- Check muxrate is 1M (not 2M)
- Verify video bitrate is 700k (not higher)
- Ensure framerate matches webcam (30 fps)

**No video on receiver:**
- Check DVB-S receiver is set to 1 Msps symbol rate
- Verify FEC is 1/2
- Ensure signal strength is good (> 80%)

**Pixelated/broken video:**
- Lower video bitrate if needed (try 500k)
- Check for RF interference
- Verify HackRF gain settings (30-47 dB)

## Future Work

To make the Go version work, we'd need:
1. Port to pure C/C++ for speed
2. Use SIMD optimizations (AVX2/NEON)
3. Implement polyphase FIR filtering like leansdr
4. Consider hardware acceleration (GPU/FPGA)

For now, the leandvbtx pipeline is the proven, working solution.
