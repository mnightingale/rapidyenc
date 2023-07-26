package encoder

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
	encodeInit()
}

type Encoder struct {
	LineLength int32
}

func NewEncoder() *Encoder {
	return &Encoder{
		LineLength: 128,
	}
}

func (e *Encoder) Encode(src []byte) ([]byte, error) {
	dst := make([]byte, MaxLength(len(src), e.LineLength))

	length := encode(
		unsafe.Pointer(&src[0]),
		unsafe.Pointer(&dst[0]),
		len(src),
	)

	return dst[:length], nil
}

//go:linkname encodeInit rapidyenc_encode_init
//go:noescape
func encodeInit()

//go:linkname encode rapidyenc_encode
//go:noescape
func encode(src, dest unsafe.Pointer, size int) int

//go:linkname MaxLength rapidyenc_encode_max_length
//go:noescape
func MaxLength(length int, lineLength int32) int
