package encoder

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
	C.rapidyenc_encode_init()
}

func MaxLength(length int, line_size int) uint {
	return uint(C.rapidyenc_encode_max_length(C.size_t(length), C.int(line_size)))
}

type Encoder struct {
	LineLength int
}

func NewEncoder() *Encoder {
	return &Encoder{
		LineLength: 128,
	}
}

func (e *Encoder) Encode(src []byte) ([]byte, error) {
	// RAPIDYENC_API size_t rapidyenc_encode(const void* __restrict src, void* __restrict dest, size_t src_length);
	// RAPIDYENC_API size_t rapidyenc_encode_ex(int line_size, int* column, const void* __restrict src, void* __restrict dest, size_t src_length, int is_end);

	dst := make([]byte, MaxLength(len(src), e.LineLength))

	encoded_size := C.rapidyenc_encode(
		unsafe.Pointer(&src[0]),
		unsafe.Pointer(&dst[0]),
		C.size_t(len(src)),
	)

	return dst[:encoded_size], nil
}
