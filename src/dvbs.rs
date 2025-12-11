// src/dvbs.rs
use crate::tables::{GF_EXP, GF_LOG, PRBS_LUT, RS_POLY};
use num_complex::Complex;
use std::collections::VecDeque;

// --- Sub-Components ---

struct Scrambler {
    idx: usize,
    packet_count: usize,
}

impl Scrambler {
    fn new() -> Self {
        Self { idx: 0, packet_count: 0 }
    }

    fn process(&mut self, packet: &[u8], out: &mut [u8]) {
        // Handle Sync Byte
        if self.packet_count == 0 {
            self.idx = 0;
            out[0] = !0x47; // Invert Sync (0xB8)
        } else {
            self.idx += 1;
            out[0] = 0x47; // Normal Sync
        }

        self.packet_count = (self.packet_count + 1) % 8;

        // Scramble Payload
        for i in 1..188 {
            out[i] = packet[i] ^ PRBS_LUT[self.idx];
            self.idx += 1;
        }
    }
}

struct Interleaver {
    fifos: Vec<VecDeque<u8>>,
}

impl Interleaver {
    fn new() -> Self {
        let mut fifos = Vec::with_capacity(12);
        for i in 0..12 {
            let len = i * 17; // Depth 12 * (204/12)
            let mut queue = VecDeque::new();
            for _ in 0..len {
                queue.push_back(0);
            }
            fifos.push(queue);
        }
        Self { fifos }
    }

    fn process(&mut self, packet: &mut [u8]) {
        for j in (0..204).step_by(12) {
            for i in 0..12 {
                let idx = j + i;
                if idx >= 204 { break; }

                let input_byte = packet[idx];

                if i != 0 {
                    self.fifos[i].push_back(input_byte);
                    if let Some(val) = self.fifos[i].pop_front() {
                        packet[idx] = val;
                    }
                }
            }
        }
    }
}

// --- Main Encoder Struct ---

pub struct DvbsEncoder {
    scrambler: Scrambler,
    interleaver: Interleaver,
    buffer: Vec<u8>, // Internal Buffer: 204 Bytes
    delay_line: u32,
}

impl DvbsEncoder {
    pub fn new() -> Self {
        Self {
            scrambler: Scrambler::new(),
            interleaver: Interleaver::new(),
            buffer: vec![0u8; 204],
            delay_line: 0,
        }
    }

    fn reed_solomon(buffer: &mut [u8]) {
        let mut tmp = [0u8; 255];
        tmp[..188].copy_from_slice(&buffer[..188]);

        for i in 0..188 {
            let coef = tmp[i];
            if coef != 0 {
                for j in 0..16 {
                    let idx = (GF_LOG[coef as usize] as u16 + GF_LOG[RS_POLY[j] as usize] as u16) as usize;
                    tmp[i + j + 1] ^= GF_EXP[idx % 511];
                }
            }
        }
        buffer[188..204].copy_from_slice(&tmp[188..204]);
    }

    pub fn encode_packet(&mut self, ts_packet: &[u8]) -> Vec<Complex<f32>> {
        // 1. Scramble
        self.scrambler.process(ts_packet, &mut self.buffer[0..188]);

        // 2. Reed-Solomon
        Self::reed_solomon(&mut self.buffer);

        // 3. Interleave
        self.interleaver.process(&mut self.buffer);

        // 4. Convolution (Viterbi) + QPSK
        let mut symbols = Vec::with_capacity(204 * 8);

        let g1 = 0x79; // 171 octal
        let g2 = 0x5b; // 133 octal

        for byte in &self.buffer {
            for j in (0..8).rev() { // Process MSB first
                let bit = (byte >> j) & 1;

                // FIX: Match C++ shift direction (New bit at MSB, shift right)
                self.delay_line |= (bit as u32) << 6;

                let x = (self.delay_line & g1).count_ones() % 2;
                let y = (self.delay_line & g2).count_ones() % 2;

                // Shift history down
                self.delay_line >>= 1;

                // QPSK Map
                let i_val = if x == 0 { 0.707 } else { -0.707 };
                let q_val = if y == 0 { 0.707 } else { -0.707 };

                symbols.push(Complex::new(i_val, q_val));
            }
        }

        symbols
    }
}