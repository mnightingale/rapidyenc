package crc32

/*
#cgo CFLAGS: -I${SRCDIR}/../rapidyenc
#cgo LDFLAGS: -L${SRCDIR}/../ -lrapidyenc
#include "rapidyenc.h"
*/
import "C"
import (
	"unsafe"
)

func init() {
	crcInit()
}

// Checksum returns the CRC32 hash of `src`, with initial CRC32 value 0
func Checksum(src []byte) uint32 {
	return Update(0, src)
}

// Update returns the CRC32 hash of `src`, with a provided initial CRC32 value
func Update(initCrc uint32, src []byte) uint32 {
	return crc(
		unsafe.Pointer(&src[0]),
		len(src),
		initCrc,
	)
}

//go:linkname crcInit rapidyenc_crc_init
//go:noescape
func crcInit()

//go:linkname crc rapidyenc_crc
//go:noescape
func crc(src unsafe.Pointer, srcLength int, initCrc uint32) uint32

// Combine two CRC32 hashes
// Given `crc1 = CRC32(data1)` and `crc2 = CRC32(data2)`, returns CRC32(data1 + data2) `length2` refers to the length of 'data2'
//
//go:linkname Combine rapidyenc_crc_combine
//go:noescape
func Combine(crc1, crc2 uint32, length2 int) uint32

//go:linkname Multiply rapidyenc_crc_multiply
//go:noescape
func Multiply(crc1, crc2 uint32) uint32

//go:linkname ZeroUnpad rapidyenc_crc_zero_unpad
//go:noescape
func ZeroUnpad(crc uint32, length int) uint32

//go:linkname XPOW8N rapidyenc_crc_xpow8n
//go:noescape
func XPOW8N(n int) uint32
