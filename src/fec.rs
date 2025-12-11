// src/fec.rs

pub struct ConvolutionalEncoder {
    history: u8, // 6 bits of history for K=7
}

impl ConvolutionalEncoder {
    pub fn new() -> Self {
        Self { history: 0 }
    }

    /// Encodes 1 input byte into 2 output bytes (Rate 1/2)
    /// Returns: (I_bits, Q_bits) packed into bytes
    pub fn encode_byte(&mut self, input: u8) -> Vec<u8> {
        let mut output_symbols = Vec::with_capacity(8); // 8 bits in -> 8 symbols out

        // Process bits MSB first
        for i in (0..8).rev() {
            let bit = (input >> i) & 1;

            // Shift into history
            // History is 6 bits. (history << 1) | bit makes 7 bits total.
            let reg = (self.history << 1) | bit;
            self.history = reg & 0b111111; // Keep only 6 bits for next state

            // Apply G1 (171 octal = 1111001 binary)
            // We mask the 7 bits (history + current) with poly
            let g1_bits = (reg as u16 | ((bit as u16) << 6)) & 0b1111001;
            let output_x = g1_bits.count_ones() % 2; // XOR sum (parity)

            // Apply G2 (133 octal = 1011011 binary)
            let g2_bits = (reg as u16 | ((bit as u16) << 6)) & 0b1011011;
            let output_y = g2_bits.count_ones() % 2;

            // DVB-S maps X -> I, Y -> Q directly for QPSK
            // We pack X and Y into a simplified "symbol" byte: 0b000000YX
            // Bit 1 = Q (Y), Bit 0 = I (X)
            output_symbols.push((output_y << 1 | output_x) as u8);
        }
        output_symbols
    }
}