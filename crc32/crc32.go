package crc32

/*
#cgo CFLAGS: -I${SRCDIR}/../rapidyenc
#cgo LDFLAGS: -L${SRCDIR}/../ -lrapidyenc
#cgo darwin LDFLAGS: -L${SRCDIR}/../ -lrapidyenc
#include "rapidyenc.h"
*/
import "C"
import (
	"unsafe"
)

func init() {
	C.rapidyenc_crc_init()
}

// Checksum returns the CRC32 hash of `src`, with initial CRC32 value 0
func Checksum(src []byte) uint32 {
	return Update(0, src)
}

// Update returns the CRC32 hash of `src`, with a provided initial CRC32 value
func Update(crc uint32, src []byte) uint32 {
	return uint32(C.rapidyenc_crc(
		unsafe.Pointer(&src[0]),
		C.size_t(len(src)),
		C.uint32_t(crc),
	))
}

// Combine two CRC32 hashes
// Given `crc1 = CRC32(data1)` and `crc2 = CRC32(data2)`, returns CRC32(data1 + data2) `length2` refers to the length of 'data2'
func Combine(crc1 uint32, crc2 uint32, length2 uint64) uint32 {
	return uint32(C.rapidyenc_crc_combine(
		C.uint32_t(crc1),
		C.uint32_t(crc2),
		C.size_t(length2),
	))
}

func Multiply(crc1 uint32, crc2 uint32) uint32 {
	return uint32(C.rapidyenc_crc_multiply(
		C.uint32_t(crc1),
		C.uint32_t(crc2),
	))
}

func ZeroUnpad(crc uint32, length uint64) uint32 {
	return uint32(C.rapidyenc_crc_zero_unpad(
		C.uint32_t(crc),
		C.size_t(length),
	))
}

func XPOW8N(n uint64) uint32 {
	return uint32(C.rapidyenc_crc_xpow8n(
		C.size_t(n),
	))
}
