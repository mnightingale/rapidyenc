//gp:build cgo

package rapidyenc

/*
#include "rapidyenc.h"
*/
import "C"
import (
	"errors"
	"sync"
	"unsafe"
)

var encodeInitOnce sync.Once

func maybeInitEncode() {
	encodeInitOnce.Do(func() {
		C.rapidyenc_encode_init()
	})
}

func encodeIncremental(lineLength int, column *int, src []byte, dest []byte, isEnd bool) []byte {
	maybeInitEncode()

	col := C.int(*column)

	length := C.rapidyenc_encode_ex(
		C.int(lineLength),
		&col,
		unsafe.Pointer(&src[0]),
		unsafe.Pointer(&dest[0]),
		C.size_t(len(src)),
		C.int(0),
	)

	*column = int(col)

	return dest[:length]
}

// Encode yEnc encodes the src buffer without adding any =y headers
//
// Deprecated: use Encoder as an io.WriteCloser which includes yEnc headers
func Encode(src []byte) ([]byte, error) {
	maybeInitEncode()

	if len(src) == 0 {
		return nil, errors.New("empty source")
	}

	dst := make([]byte, maxLength(len(src), 128))

	length := C.rapidyenc_encode(
		unsafe.Pointer(&src[0]),
		unsafe.Pointer(&dst[0]),
		C.size_t(len(src)),
	)

	return dst[:length], nil
}
