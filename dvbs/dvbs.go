package dvbs

import (
	"io"
	"log"

	"hackdvbs/consts"
	"hackdvbs/filter"
	"hackdvbs/utils"
)

// DVB-S encoder
type DVBSEncoder struct {
	rsEncoder      *RSEncoder // Use our custom RS encoder
	rsPacket       []byte
	interleaverRAM []byte // RAM for the convolutional interleaver
}

// Create new encoder with RS(204,188)
func NewDVBSEncoder() *DVBSEncoder {
	// Initialize our custom, DVB-S compliant Reed-Solomon encoder.
	rsEnc := NewRSEncoder()

	interleaverRAM := make([]byte, consts.InterleaveDepth*consts.RSPacketSize/consts.InterleaveDepth*(consts.InterleaveDepth-1)/2)

	return &DVBSEncoder{
		rsEncoder:      rsEnc,
		rsPacket:       make([]byte, consts.RSPacketSize),
		interleaverRAM: interleaverRAM,
	}
}

// PRBS scrambler per DVB-S spec (ETSI EN 300 421).
func (e *DVBSEncoder) Scramble(ts []byte) []byte {
	out := make([]byte, consts.TSPacketSize)
	out[0] = ts[0] ^ 0xFF
	state := uint16(0x4A80)
	for i := 1; i < consts.TSPacketSize; i++ {
		var scrambleByte byte
		for j := 0; j < 8; j++ {
			outputBit := byte((state >> 14) & 1)
			scrambleByte = (scrambleByte << 1) | outputBit
			newbit := ((state >> 14) ^ (state >> 13)) & 1
			state = ((state << 1) | newbit) & 0x7FFF
		}
		out[i] = ts[i] ^ scrambleByte
	}
	return out
}

// Reed-Solomon RS(204,188) encoding using our custom encoder.
func (e *DVBSEncoder) ReedSolomon(scrambled []byte) {
	encodedPacket := e.rsEncoder.Encode(scrambled)
	copy(e.rsPacket, encodedPacket)
}

// DVB-S convolutional interleaver (Forney).
func (e *DVBSEncoder) Interleave() {
	const I = consts.InterleaveDepth
	const M = consts.RSPacketSize / I
	interleaved := make([]byte, consts.RSPacketSize)
	ramOffset := 0
	for j := 0; j < I; j++ {
		for q := 0; q < M; q++ {
			addr := q*I + j
			if j > 0 {
				ramAddr := ramOffset + q*j
				interleaved[addr] = e.interleaverRAM[ramAddr]
				e.interleaverRAM[ramAddr] = e.rsPacket[addr]
			} else {
				interleaved[addr] = e.rsPacket[addr]
			}
		}
		if j > 0 {
			ramOffset += M * j
		}
	}
	copy(e.rsPacket, interleaved)
}

// Convolutional coding (rate 1/2, DVB-S).
func (e *DVBSEncoder) ConvolutionalEncode() []byte {
	const g1 = 0x79
	const g2 = 0x5B
	delay := uint16(0)
	out := make([]byte, 0, consts.RSPacketSize*8*2)
	for i := 0; i < consts.RSPacketSize; i++ {
		b := e.rsPacket[i]
		// Process bits LSB-first as required by the standard.
		for j := 0; j < 8; j++ {
			bit := (b >> uint(j)) & 1
			delay = ((delay << 1) | uint16(bit)) & 0x7F
			b1 := utils.Parity(delay & g1)
			b2 := utils.Parity(delay & g2)
			out = append(out, b1, b2)
		}
	}
	return out
}

// Full DVB-S encoding pipeline
func (e *DVBSEncoder) EncodePacket(tsPacket []byte) []byte {
	scrambled := e.Scramble(tsPacket)
	e.ReedSolomon(scrambled)
	e.Interleave()
	return e.ConvolutionalEncode()
}

// StreamToIQ: reads TS packets, encodes, maps, filters, and fills iqBuffer
func StreamToIQ(ffmpegStdout io.Reader, iqBuffer chan complex128, dvbsEncoder *DVBSEncoder, rrcFilter *filter.FIRFilter) {
	defer close(iqBuffer)
	tsPacket := make([]byte, consts.TSPacketSize)
	var lastI_in, lastQ_in float64
	var lastI_out, lastQ_out float64
	const alpha = 0.999
	for {
		_, err := io.ReadFull(ffmpegStdout, tsPacket)
		if err != nil {
			log.Println("FFmpeg stream ended.")
			return
		}
		if tsPacket[0] != consts.TSSyncByte {
			log.Println("Warning: Lost TS packet sync.")
			continue
		}
		encodedBits := dvbsEncoder.EncodePacket(tsPacket)
		qpskSymbols := make([]complex128, len(encodedBits)/2)
		for i := 0; i < len(encodedBits); i += 2 {
			sym := (encodedBits[i] << 1) | encodedBits[i+1]
			qpskSymbols[i/2] = consts.QPSKSymbolMap[sym]
		}
		iqSamples := rrcFilter.Process(qpskSymbols)
		// CORRECTED: Fixed typo in iqSamples variable.
		for _, sample := range iqSamples {
			real_in := real(sample)
			imag_in := imag(sample)
			real_out := real_in - lastI_in + alpha*lastI_out
			imag_out := imag_in - lastQ_in + alpha*lastQ_out
			lastI_in = real_in
			lastQ_in = imag_in
			lastI_out = real_out
			lastQ_out = imag_out
			iqBuffer <- complex(real_out, imag_out)
		}
	}
}