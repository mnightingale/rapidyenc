//go:build arm64 && !cgo

package rapidyenc

import (
	"hash/crc32"

	"golang.org/x/sys/cpu"
)

// The arm64 CRC32 instructions have several cycles of latency but issue once
// per cycle, so a single accumulator runs at a fraction of the achievable
// rate. Large buffers are split into independent streams whose partial
// registers are combined afterwards.

var (
	crcMultiStream bool
	crcQuarters    = [2]int{crcQuarterBig, crcQuarterSml}
	crcQuarterK    [2]uint32
)

func init() {
	crcMultiStream = cpu.ARM64.HasCRC32
	for i, q := range crcQuarters {
		crcQuarterK[i] = shiftZeros(0x80000000, q)
	}
}

//go:noescape
func crc32IEEEQuad(crcs *[4]uint32, p []byte)

// crcUpdate is crc32.Update for the IEEE polynomial.
func crcUpdate(crc uint32, p []byte) uint32 {
	if !crcMultiStream || len(p) < crcMinChunk {
		return crc32.Update(crc, crc32.IEEETable, p)
	}

	raw := ^crc
	for i, q := range crcQuarters {
		chunk, k := crcStreams*q, crcQuarterK[i]
		for len(p) >= chunk {
			c := [4]uint32{raw, 0, 0, 0}
			crc32IEEEQuad(&c, p[:chunk])
			raw = crcMul(c[0], k) ^ c[1]
			raw = crcMul(raw, k) ^ c[2]
			raw = crcMul(raw, k) ^ c[3]
			p = p[chunk:]
		}
	}

	return crc32.Update(^raw, crc32.IEEETable, p)
}
