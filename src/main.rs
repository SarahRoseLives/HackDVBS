// src/main.rs
mod tables;
mod dvbs;

use anyhow::Result;
use num_complex::Complex;
use soapysdr::Direction::Tx;
use std::io::Read;
use std::sync::mpsc::{channel, Receiver, TryRecvError};
use std::thread;
use crate::dvbs::DvbsEncoder;

// SETTINGS
const FREQ: f64 = 1_280_000_000.0;
const SAMPLE_RATE: f64 = 2_000_000.0; // 2 MSps
const BW: f64 = 2_000_000.0;
const GAIN: f64 = 30.0;

fn main() -> Result<()> {
    // 1. Setup Hardware
    println!("Init HackRF: {:.1} MHz @ {:.1} MSps", FREQ/1e6, SAMPLE_RATE/1e6);
    let dev = soapysdr::Device::new("driver=hackrf")?;

    dev.set_frequency(Tx, 0, FREQ, "")?;
    dev.set_sample_rate(Tx, 0, SAMPLE_RATE)?;
    dev.set_bandwidth(Tx, 0, BW)?;
    dev.set_gain(Tx, 0, GAIN)?;
    dev.write_setting("AMP", "1")?;

    let mut tx_stream = dev.tx_stream::<Complex<f32>>(&[0])?;
    let mtu = tx_stream.mtu().unwrap_or(16384);

    // 2. Setup Threading Channel (FFmpeg -> Main Loop)
    let (tx_queue, rx_queue): (std::sync::mpsc::Sender<[u8; 188]>, Receiver<[u8; 188]>) = channel();

    // 3. Spawn Input Thread (Reads FFmpeg stdin)
    thread::spawn(move || {
        let mut stdin = std::io::stdin();
        loop {
            let mut packet = [0u8; 188];
            if let Err(_) = stdin.read_exact(&mut packet) {
                break;
            }
            if tx_queue.send(packet).is_err() {
                break;
            }
        }
    });

    // 4. Main Transmission Loop
    let mut encoder = DvbsEncoder::new(); // <-- The new full DVB-S chain
    let mut iq_buffer = Vec::with_capacity(mtu);

    println!("Transmitting DVB-S (FEC 1/2)...");

    let mut packet = [0u8; 188];

    loop {
        // Try to get real data
        match rx_queue.try_recv() {
            Ok(real_data) => {
                packet = real_data;
            },
            Err(TryRecvError::Empty) => {
                // Stuffing if buffer empty
                generate_null_packet(&mut packet);
            },
            Err(TryRecvError::Disconnected) => break,
        }

        // ENCODE (Scramble -> RS -> Interleave -> Conv -> QPSK)
        let symbols = encoder.encode_packet(&packet);

        // UPSAMPLE 2x (1 MSps -> 2 MSps)
        for sym in symbols {
            iq_buffer.push(sym);
            iq_buffer.push(sym);
        }

        // WRITE TO HARDWARE
        if iq_buffer.len() >= mtu {
            match tx_stream.write(&[&iq_buffer], None, false, 1_000_000) {
                 Ok(_) => (),
                 Err(e) => {
                     if e.code != soapysdr::ErrorCode::Underflow {
                         eprintln!("TX: {}", e);
                     }
                 }
            }
            iq_buffer.clear();
        }
    }

    Ok(())
}

fn generate_null_packet(packet: &mut [u8; 188]) {
    packet[0] = 0x47;
    packet[1] = 0x1F;
    packet[2] = 0xFF;
    packet[3] = 0x10;
    for i in 4..188 {
        packet[i] = 0xFF;
    }
}