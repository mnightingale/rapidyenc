package decoder

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
	decodeInit()
}

func Decode(src []byte) []byte {
	length := decode(
		unsafe.Pointer(&src[0]),
		unsafe.Pointer(&src[0]),
		len(src),
	)

	return src[:length]
}

//go:linkname decodeInit rapidyenc_decode_init
//go:noescape
func decodeInit()

//go:linkname decode rapidyenc_decode
//go:noescape
func decode(src, dest unsafe.Pointer, size int) int
