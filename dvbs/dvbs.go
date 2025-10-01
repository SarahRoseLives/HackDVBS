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
	rsEncoder          *RSEncoder
	interleaverFIFOs   [][]byte
	interleaverIndices []int
	prbsIndex          int
	packetCounter      int
}

// NewDVBSEncoder creates a new encoder.
func NewDVBSEncoder() *DVBSEncoder {
	rsEnc := NewRSEncoder()
	const I = consts.InterleaveDepth
	const M = consts.RSPacketSize / I
	fifos := make([][]byte, I)
	indices := make([]int, I)
	for i := 1; i < I; i++ {
		fifos[i] = make([]byte, i*M)
	}
	return &DVBSEncoder{
		rsEncoder:          rsEnc,
		interleaverFIFOs:   fifos,
		interleaverIndices: indices,
		prbsIndex:          0,
		packetCounter:      0,
	}
}

// ScrambleTS scrambles a 188-byte TS packet to be bug-for-bug compatible with SDRangel.
func (e *DVBSEncoder) ScrambleTS(tsPacket []byte) []byte {
	scrambledPacket := make([]byte, consts.TSPacketSize)
	copy(scrambledPacket, tsPacket)

	// This logic matches the peculiar SDRangel implementation.
	if e.packetCounter == 0 {
		e.prbsIndex = 0 // Reset PRBS index for the first packet in a group of 8.
		scrambledPacket[0] = ^scrambledPacket[0] // Invert sync byte.
	} else {
		// For packets 1-7, the PRBS index is incremented ONCE before the per-byte scrambling.
		// THIS IS THE CRITICAL "BUG" TO REPLICATE.
		e.prbsIndex++
	}

	// The PRBS sequence is applied to the payload (bytes 1 to 187).
	// We use a temporary index to ensure the "off-by-one" packet-level increment is handled correctly.
	currentPrbsIndex := e.prbsIndex
	for i := 1; i < consts.TSPacketSize; i++ {
		// Wrap the index if it exceeds the LUT length.
		if currentPrbsIndex >= len(PrbsLUT) {
			currentPrbsIndex = 0
		}
		scrambledPacket[i] ^= PrbsLUT[currentPrbsIndex]
		currentPrbsIndex++
	}
	// Update the encoder's state with the final index for the next packet.
	e.prbsIndex = currentPrbsIndex

	e.packetCounter = (e.packetCounter + 1) % 8
	return scrambledPacket
}

// ReedSolomon encodes the 188-byte packet into a 204-byte RS packet.
func (e *DVBSEncoder) ReedSolomon(packet []byte) []byte {
	return e.rsEncoder.Encode(packet)
}

// Interleave performs convolutional interleaving on the 204-byte RS packet.
func (e *DVBSEncoder) Interleave(rsPacket []byte) []byte {
	out := make([]byte, consts.RSPacketSize)
	copy(out, rsPacket)

	const I = consts.InterleaveDepth
	p := 0
	for j := 0; j < consts.RSPacketSize; j += I {
		p++
		for i := 1; i < I; i++ {
			if p < consts.RSPacketSize {
				fifo := e.interleaverFIFOs[i]
				idx := e.interleaverIndices[i]
				tmp := fifo[idx]
				fifo[idx] = out[p]
				out[p] = tmp
				e.interleaverIndices[i] = (idx + 1) % len(fifo)
				p++
			}
		}
	}
	return out
}

// ConvolutionalEncode performs rate 1/2 FEC.
func (e *DVBSEncoder) ConvolutionalEncode(interleavedPacket []byte) []byte {
	// Use bit-reversed generator polynomials to match the left-shifting
	// register implementation with the original SDRangel C++ (right-shifting) output.
	const g1 = 0x4F // Reversed 0x79
	const g2 = 0x6D // Reversed 0x5B

	delay := uint16(0)
	out := make([]byte, 0, consts.RSPacketSize*8*2)
	for i := 0; i < consts.RSPacketSize; i++ {
		b := interleavedPacket[i]
		for j := 7; j >= 0; j-- {
			bit := (b >> uint(j)) & 1
			delay = ((delay << 1) | uint16(bit)) & 0x7F // This left-shift is correct
			b1 := utils.Parity(delay & g1)
			b2 := utils.Parity(delay & g2)
			out = append(out, b1, b2)
		}
	}
	return out
}

// EncodePacket runs the full DVB-S pipeline in the correct standard order.
func (e *DVBSEncoder) EncodePacket(tsPacket []byte) []byte {
	// 1. Scramble the 188-byte TS packet
	scrambledPacket := e.ScrambleTS(tsPacket)

	// 2. Add Reed-Solomon parity bytes
	rsPacket := e.ReedSolomon(scrambledPacket)

	// 3. Interleave the 204-byte packet
	interleavedPacket := e.Interleave(rsPacket)

	// 4. Convolve the interleaved packet
	return e.ConvolutionalEncode(interleavedPacket)
}

// StreamToIQ processes the TS stream and generates I/Q samples.
func StreamToIQ(tsReader io.Reader, iqBuffer chan complex128, dvbsEncoder *DVBSEncoder, rrcFilter *filter.FIRFilter) {
	defer close(iqBuffer)
	tsPacket := make([]byte, consts.TSPacketSize)

	for {
		_, err := io.ReadFull(tsReader, tsPacket)
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading TS stream: %v", err)
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

		// Pass samples directly to the buffer without any DC blocking.
		for _, sample := range iqSamples {
			iqBuffer <- sample
		}
	}
}