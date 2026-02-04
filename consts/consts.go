package consts

const (
	SymbolRate       = 1000000.0
	HackRFSampleRate = 2000000.0
	RollOffFactor    = 0.35
	TSPacketSize     = 188
	RSPacketSize     = 204
	RRCFilterTaps    = 41  // Reduced for faster processing
	InterleaveDepth  = 12
	TSSyncByte       = 0x47
)