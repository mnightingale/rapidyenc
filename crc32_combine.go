package rapidyenc

import "hash/crc32"

// Bytes per stream. The combine cost is fixed per chunk, so a large quarter
// is used first and a small one mops up the remainder that would otherwise
// fall back to the byte-at-a-time path.
const (
	crcStreams    = 4
	crcQuarterBig = 2048
	crcQuarterSml = 256

	crcChunkSize = crcStreams * crcQuarterBig
	crcMinChunk  = crcStreams * crcQuarterSml
)

// Helpers for combining CRC32 (IEEE) values computed over adjacent blocks.
//
// These work on the raw, non-inverted CRC register, which is what the arm64
// CRC32 instructions operate on; hash/crc32 exposes the inverted form.
//
// Feeding data through the register is linear:
//
//	raw(init, a||b) = raw(raw(init, a), b)
//	raw(init, data) = raw(init, zeros) ^ raw(0, data)
//
// so two blocks combine as shiftZeros(crcA, len(b)) ^ crcB, and shiftZeros is
// a carry-less multiply by x^(8*len) modulo the CRC polynomial.

// rawUpdate advances the non-inverted register over p.
func rawUpdate(crc uint32, p []byte) uint32 {
	for _, v := range p {
		crc = crc32.IEEETable[byte(crc)^v] ^ (crc >> 8)
	}
	return crc
}

// crcMul multiplies two CRC values in the register's GF(2) representation.
func crcMul(a, b uint32) uint32 {
	p := clmul32(a, b) << 1
	return rawUpdate(0, []byte{byte(p), byte(p >> 8), byte(p >> 16), byte(p >> 24)}) ^ uint32(p>>32)
}

// shiftZeros advances the register over n zero bytes, the slow way.
func shiftZeros(crc uint32, n int) uint32 {
	return rawUpdate(crc, make([]byte, n))
}
