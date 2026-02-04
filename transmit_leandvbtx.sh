#!/bin/bash
# DVB-S transmitter using leandvbtx pipeline
# This is the working solution with 0 underruns

FREQ=${1:-1250000000}
GAIN=${2:-47}
VIDEO_DEVICE=${3:-/dev/video0}
AUDIO_DEVICE=${4:-default}

echo "DVB-S Transmitter (leandvbtx pipeline)"
echo "Frequency: $FREQ Hz"
echo "TX VGA Gain: $GAIN dB"
echo "Video Device: $VIDEO_DEVICE"
echo "Audio Device: $AUDIO_DEVICE"
echo ""

# Check if tools exist
if ! command -v /tmp/leansdr/src/apps/leandvbtx &> /dev/null; then
    echo "Error: leandvbtx not found. Building..."
    cd /tmp && git clone --depth 1 https://github.com/pabr/leansdr.git
    cd /tmp/leansdr/src/apps && make leandvbtx
fi

if ! [ -f /tmp/s16_to_s8 ]; then
    echo "Building s16_to_s8 converter..."
    cat > /tmp/s16_to_s8.c << 'EOF'
// Convert interleaved signed 16-bit I/Q to signed 8-bit
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
            buf8[i] = buf16[i] >> 8;
        }
        write(1, buf8, samples);
    }
    return 0;
}
EOF
    gcc -O3 /tmp/s16_to_s8.c -o /tmp/s16_to_s8
fi

echo "Starting transmission..."
ffmpeg -hide_banner -loglevel warning \
  -thread_queue_size 512 -f v4l2 -video_size 640x480 -framerate 30 -i "$VIDEO_DEVICE" \
  -thread_queue_size 512 -f alsa -i "$AUDIO_DEVICE" \
  -r 30 \
  -c:v mpeg2video -pix_fmt yuv420p -b:v 700k -maxrate 700k -bufsize 1400k -g 10 -bf 0 \
  -c:a mp2 -b:a 128k -ar 44100 \
  -f mpegts -muxrate 1M -pcr_period 20 - 2>/dev/null | \
/tmp/leansdr/src/apps/leandvbtx --s16 --cr 1/2 -f 2 2>/dev/null | \
/tmp/s16_to_s8 | \
hackrf_transfer -t - -f "$FREQ" -s 2000000 -x "$GAIN" -a 1 -B
