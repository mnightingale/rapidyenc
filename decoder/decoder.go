package decoder

/*
#cgo CFLAGS: -I${SRCDIR}/../rapidyenc
#cgo LDFLAGS: -L${SRCDIR}/../ -lrapidyenc
#include "rapidyenc.h"
*/
import "C"
import (
	"errors"
	"unsafe"
)

func init() {
	C.rapidyenc_decode_init()
}

// State is the current decoder state, for incremental decoding
// The values here refer to the previously seen characters in the stream, which influence how some sequences need to be handled
// The shorthands represent:
// CR (\r), LF (\n), EQ (=), DT (.)
type State int

const (
	StateCRLF     State = C.RYDEC_STATE_CRLF
	StateEQ       State = C.RYDEC_STATE_EQ
	StateCR       State = C.RYDEC_STATE_CR
	StateNone     State = C.RYDEC_STATE_NONE
	StateCRLFDT   State = C.RYDEC_STATE_CRLFDT
	StateCRLFDTCR State = C.RYDEC_STATE_CRLFDTCR
	StateCRLFEQ   State = C.RYDEC_STATE_CRLFEQ // may actually be "\r\n.=" in raw decoder
)

// end is the state for incremental decoding, whether the end of the yEnc data was reached
const (
	endNone    = C.RYDEC_END_NONE    // end not reached
	endControl = C.RYDEC_END_CONTROL // \r\n=y sequence found, src points to byte after 'y'
	endArticle = C.RYDEC_END_ARTICLE // \r\n.\r\n sequence found, src points to byte after last '\n'
)

var (
	ErrDestinationTooSmall = errors.New("destination must be at least the length of source")
)

// DecodeIncremental stops decoding when a yEnc/NNTP end sequence is found
func DecodeIncremental(dst, src []byte, state *State) (int, int, bool) {
	if len(dst) < len(src) {
		panic(ErrDestinationTooSmall)
	}

	srcPointer := uintptr(unsafe.Pointer(&src[0]))
	dstPointer := uintptr(unsafe.Pointer(&dst[0]))

	end := C.rapidyenc_decode_incremental(
		(*unsafe.Pointer)(unsafe.Pointer(&srcPointer)),
		(*unsafe.Pointer)(unsafe.Pointer(&dstPointer)),
		C.size_t(len(src)),
		(*C.RapidYencDecoderState)(unsafe.Pointer(state)),
	)

	nSrc := int(srcPointer - uintptr(unsafe.Pointer(&src[0])))
	nDst := int(dstPointer - uintptr(unsafe.Pointer(&dst[0])))

	if end == endControl {
		nSrc -= len("=y")
	} else if end == endArticle {
		nSrc -= len("./r/n")
	}

	return nSrc, nDst, end != endNone
}
