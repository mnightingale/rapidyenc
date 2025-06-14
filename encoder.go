package rapidyenc

/*
#cgo darwin LDFLAGS: ${SRCDIR}/librapidyenc_darwin.a -lstdc++
#cgo windows,amd64 LDFLAGS: ${SRCDIR}/librapidyenc_windows_amd64.a -lstdc++
#cgo linux,amd64 LDFLAGS: ${SRCDIR}/librapidyenc_linux_amd64.a -lstdc++
#cgo linux,arm64 LDFLAGS: ${SRCDIR}/librapidyenc_linux_arm64.a -lstdc++
#include "rapidyenc.h"
*/
import "C"
import (
	"sync"
	"unsafe"
)

// MaxLength returns the maximum possible length of yEnc encoded output given the length of the unencoded data and line length.
// This function also includes additional padding needed by rapidyenc's implementation.
func MaxLength(length, lineLength int) int {
	ret := length * 2 // all characters escaped
	ret += 2          // allocation for offset and that a newline may occur early
	ret += 64         // allocation for YMM overflowing

	// add newlines, considering the possibility of all chars escaped
	if lineLength == 128 {
		// optimize common case
		return ret + 2*(length>>6)
	}
	return ret + 2*((length*2)/lineLength)
}

type Encoder struct {
	LineLength int
}

func NewEncoder() *Encoder {
	return &Encoder{
		LineLength: 128,
	}
}

var encodeInitOnce sync.Once

func maybeInitEncode() {
	encodeInitOnce.Do(func() {
		C.rapidyenc_encode_init()
	})
}

func (e *Encoder) Encode(src []byte) []byte {
	maybeInitEncode()

	dst := make([]byte, MaxLength(len(src), e.LineLength))

	length := C.rapidyenc_encode(
		unsafe.Pointer(&src[0]),
		unsafe.Pointer(&dst[0]),
		C.size_t(len(src)),
	)

	return dst[:length]
}
