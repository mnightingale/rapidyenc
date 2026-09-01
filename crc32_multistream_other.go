//go:build !arm64 || cgo

package rapidyenc

import "hash/crc32"

// crcUpdate is crc32.Update for the IEEE polynomial.
func crcUpdate(crc uint32, p []byte) uint32 {
	return crc32.Update(crc, crc32.IEEETable, p)
}
