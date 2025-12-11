// src/scrambler.rs

pub struct DvbsScrambler {
    lfsr: u16,
    packet_counter: u8,
}

impl DvbsScrambler {
    pub fn new() -> Self {
        Self {
            // FIX: Added '0b' prefix so Rust treats this as binary, not decimal
            lfsr: 0b100101010000000,
            packet_counter: 0,
        }
    }

    /// Scrambles an MPEG-TS packet (188 bytes)
    pub fn scramble(&mut self, packet: &mut [u8]) {
        if packet.len() != 188 {
            return;
        }

        // Handle Sync Byte Inversion (0x47 -> 0xB8 every 8 packets)
        if self.packet_counter == 0 {
            packet[0] = 0xB8;
            self.reset_lfsr();
        } else {
            packet[0] = 0x47;
        }

        self.packet_counter = (self.packet_counter + 1) % 8;

        // Scramble payload
        for i in 1..188 {
            packet[i] ^= self.get_byte();
        }
    }

    fn reset_lfsr(&mut self) {
        // FIX: Added '0b' here too
        self.lfsr = 0b100101010000000;
    }

    fn get_byte(&mut self) -> u8 {
        let mut byte = 0u8;
        for i in (0..8).rev() {
            let bit = ((self.lfsr >> 14) ^ (self.lfsr >> 13)) & 1;
            self.lfsr = (self.lfsr << 1) | bit;
            byte |= (bit as u8) << i;
        }
        byte
    }
}