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
	rsEncoder      *RSEncoder
	rsPacket       []byte
	interleaverRAM []byte
	prbsCounter    int // Add a counter to cycle through the PRBS LUT
}

// NewDVBSEncoder creates a new encoder.
func NewDVBSEncoder() *DVBSEncoder {
	rsEnc := NewRSEncoder()
	interleaverRAM := make([]byte, consts.InterleaveDepth*consts.RSPacketSize/consts.InterleaveDepth*(consts.InterleaveDepth-1)/2)

	return &DVBSEncoder{
		rsEncoder:      rsEnc,
		rsPacket:       make([]byte, consts.RSPacketSize),
		interleaverRAM: interleaverRAM,
		prbsCounter:    0, // Initialize the counter
	}
}

// ReedSolomon encodes the 188-byte TS packet into a 204-byte RS packet.
func (e *DVBSEncoder) ReedSolomon(tsPacket []byte) {
	encodedPacket := e.rsEncoder.Encode(tsPacket)
	copy(e.rsPacket, encodedPacket)
}

// Scramble performs energy dispersal on a 204-byte Reed-Solomon packet using the LUT.
func (e *DVBSEncoder) Scramble() {
	// The sync byte of the original TS packet is inverted.
	e.rsPacket[0] ^= 0xFF // 0x47 becomes 0xB8

	// The remaining 203 bytes are scrambled using the PRBS look-up table.
	for i := 1; i < consts.RSPacketSize; i++ {
		e.rsPacket[i] ^= PrbsLUT[e.prbsCounter]
		e.prbsCounter = (e.prbsCounter + 1) % len(PrbsLUT)
	}
}

// Interleave performs convolutional interleaving.
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

// ConvolutionalEncode performs rate 1/2 FEC.
func (e *DVBSEncoder) ConvolutionalEncode() []byte {
	const g1 = 0x79
	const g2 = 0x5B
	delay := uint16(0)
	out := make([]byte, 0, consts.RSPacketSize*8*2)
	for i := 0; i < consts.RSPacketSize; i++ {
		b := e.rsPacket[i]
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

// EncodePacket runs the full DVB-S pipeline.
func (e *DVBSEncoder) EncodePacket(tsPacket []byte) []byte {
	e.ReedSolomon(tsPacket)
	e.Scramble()
	e.Interleave()
	return e.ConvolutionalEncode()
}

// StreamToIQ processes the TS stream and generates I/Q samples.
func StreamToIQ(ffmpegStdout io.Reader, iqBuffer chan complex128, dvbsEncoder *DVBSEncoder, rrcFilter *filter.FIRFilter) {
	defer close(iqBuffer)
	tsPacket := make([]byte, consts.TSPacketSize)
	var lastI_in, lastQ_in, lastI_out, lastQ_out float64
	const alpha = 0.999
	for {
		_, err := io.ReadFull(ffmpegStdout, tsPacket)
		if err != nil {
			if err != io.EOF {
				log.Println("FFmpeg stream ended.")
			}
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
		for _, sample := range iqSamples {
			real_in := real(sample)
			imag_in := imag(sample)
			real_out := real_in - lastI_in + alpha*lastI_out
			imag_out := imag_in - lastQ_in + alpha*lastQ_out
			lastI_in, lastQ_in, lastI_out, lastQ_out = real_in, imag_in, real_out, imag_out
			iqBuffer <- complex(real_out, imag_out)
		}
	}
}