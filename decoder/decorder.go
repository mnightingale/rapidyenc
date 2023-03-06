package decoder

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
	C.rapidyenc_decode_init()
}

func Decode(src []byte) []byte {
	length := int(C.rapidyenc_decode(
		unsafe.Pointer(&src[0]),
		unsafe.Pointer(&src[0]),
		C.size_t(len(src)),
	))

	return src[:length]
}
