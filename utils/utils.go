package utils

import (
	"bufio"
	"io"
	"log"
)

func LogFFmpeg(ffmpegStderr io.Reader) {
	scanner := bufio.NewScanner(ffmpegStderr)
	for scanner.Scan() {
		log.Printf("[ffmpeg] %s", scanner.Text())
	}
}

// Parity returns 1 if the number of set bits is odd, else 0
func Parity(n uint16) byte {
	n ^= n >> 8
	n ^= n >> 4
	n ^= n >> 2
	n ^= n >> 1
	return byte(n & 1)
}