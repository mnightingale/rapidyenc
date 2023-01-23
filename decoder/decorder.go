package decoder

/*
#cgo CFLAGS: -I${SRCDIR}/../rapidyenc
#cgo LDFLAGS: -L${SRCDIR}/../ -lrapidyenc
#cgo darwin LDFLAGS: -L${SRCDIR}/../lib -lrapidyenc
#include "rapidyenc.h"
*/
import "C"
import (
	"unsafe"
)

func init() {
	C.rapidyenc_decode_init()
}

func Decode(src []byte) C.size_t {
	return C.rapidyenc_decode(
		unsafe.Pointer(&src[0]),
		unsafe.Pointer(&src[0]),
		C.size_t(len(src)),
	)
}

func DecodeIndirect(src []byte, dst []byte) C.size_t {
	if len(src) > len(dst) {
		panic("dst is smaller than src")
	}

	return C.rapidyenc_decode(
		unsafe.Pointer(&src[0]),
		unsafe.Pointer(&dst[0]),
		C.size_t(len(src)),
	)
}
