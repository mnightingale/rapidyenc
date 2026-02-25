//go:build cgo

package rapidyenc

/*
#include "rapidyenc.h"

// Like `rapidyenc_decode_incremental` but handle the pointer arithmetic
RapidYencDecoderEnd rapidyenc_decode_incremental_go(const void* src, void* dest, size_t src_length, size_t* n_src, size_t* n_dest, RapidYencDecoderState* state) {
    const void* in_ptr = src;
    void* out_ptr = dest;

    RapidYencDecoderEnd ended = rapidyenc_decode_incremental(&in_ptr, &out_ptr, src_length, state);
    *n_src = (uintptr_t)in_ptr - (uintptr_t)src;
    *n_dest = (uintptr_t)out_ptr - (uintptr_t)dest;

	return ended;
}
*/
import "C"
import (
	"sync"
	"unsafe"
)

var decodeInitOnce sync.Once

func maybeInitDecode() {
	decodeInitOnce.Do(func() {
		C.rapidyenc_decode_init()
	})
}

// decodeIncremental stops decoding when a yEnc/NNTP end sequence is found
func decodeIncremental(dst, src []byte, state *State) (nSrc int, decoded []byte, end End, err error) {
	maybeInitDecode()

	if len(src) == 0 {
		return 0, nil, EndNone, nil
	}

	if len(dst) < len(src) {
		return 0, nil, 0, errDestinationTooSmall
	}

	var cnSrc, cnDest C.size_t

	result := End(C.rapidyenc_decode_incremental_go(
		unsafe.Pointer(&src[0]),
		unsafe.Pointer(&dst[0]),
		C.size_t(len(src)),
		&cnSrc,
		&cnDest,
		(*C.RapidYencDecoderState)(unsafe.Pointer(state)),
	))

	return int(cnSrc), dst[:cnDest], result, nil
}
