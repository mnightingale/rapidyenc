//go:build !cgo && goexperiment.simd

package rapidyenc

import (
	"bytes"
	"unsafe"
)

func decodeSIMD(
	width int,
	dest []byte,
	src []byte,
	state *State,
	kernel func(dest, src []byte, escFirst uint64, nextMask uint16) (int, int, uint64, uint16),
) (nSrc int, decoded []byte, end End, err error) {
	const isRaw = true
	const searchEnd = true
	length := len(src)

	if length <= width*2 {
		return decodeGeneric(dest, src, state)
	}

	consumed := 0
	produced := 0

	pState := state
	if pState == nil {
		pState = new(StateCRLF)
	}

	if alignment := width - int(uintptr(unsafe.Pointer(&src[0]))&uintptr(width-1)); alignment != width {
		length -= alignment
		nSrc, decoded, end, err = decodeGeneric(dest, src[:alignment], pState)
		if end != EndNone {
			return nSrc, decoded, end, err
		}
		consumed += nSrc
		produced += len(decoded)
		src = src[nSrc:]
	}

	lenBuffer := width - 1
	if searchEnd {
		lenBuffer += 3
		if isRaw {
			lenBuffer += 1
		}
	} else if isRaw {
		lenBuffer += 3
	}

	if length > lenBuffer {
		// Core SIMD logic
		var nextMask uint16

		switch *pState {
		case StateCRLF:
			if isRaw && src[0] == '.' {
				nextMask = 1
				if searchEnd && bytes.Equal(src[1:], []byte("\r\n")) {
					*pState = StateCRLF
					return 3, dest[:0], EndArticle, nil
				}
				if searchEnd && bytes.Equal(src[1:], []byte("=y")) {
					*pState = StateNone
					return 3, dest[:0], EndControl, nil
				}
			} else if searchEnd && bytes.Equal(src, []byte("=y")) {
				*pState = StateNone
				return 2, dest[:0], EndControl, nil
			}
		case StateCR:
			if isRaw && len(src) >= 2 && src[0] == '\n' && src[1] == '.' {
				nextMask = 2
				if searchEnd && bytes.Equal(src[2:], []byte("\r\n")) {
					*pState = StateCRLF
					return 4, dest[:0], EndArticle, nil
				}
				if searchEnd && bytes.Equal(src[2:], []byte("=y")) {
					*pState = StateNone
					return 4, dest[:0], EndControl, nil
				}
			} else if searchEnd && bytes.Equal(src[2:], []byte("\n=y")) {
				*pState = StateNone
				return 3, dest[:0], EndControl, nil
			}
		case StateCRLFDT:
			if searchEnd && bytes.Equal(src, []byte("\r\n")) {
				*pState = StateCRLF
				return 2, dest[:0], EndArticle, nil
			}
			if searchEnd && bytes.Equal(src, []byte("=y")) {
				*pState = StateNone
				return 2, dest[:0], EndControl, nil
			}
		case StateCRLFDTCR:
			if searchEnd && bytes.Equal(src, []byte("\n")) {
				*pState = StateCRLF
				return 1, dest[:0], EndArticle, nil
			}
		case StateCRLFEQ:
			if searchEnd && bytes.Equal(src, []byte("y")) {
				*pState = StateNone
				return 1, dest[:0], EndControl, nil
			}
		}

		var escFirst uint64
		if *pState == StateEQ || *pState == StateCRLFEQ {
			escFirst = 1
		}

		c, p, escFirst, nextMask := kernel(dest[produced:], src, escFirst, nextMask)
		consumed += c
		produced += p
		src = src[c:]
		length -= c

		switch {
		case escFirst > 0:
			*pState = StateEQ
		case nextMask == 1:
			*pState = StateCRLF
		case nextMask == 2:
			*pState = StateCR
		default:
			*pState = StateNone
		}
	}

	if length > 0 {
		c, decoded, end, err := decodeGeneric(dest[produced:], src, pState)
		consumed += c
		produced += len(decoded)
		return consumed, dest[:produced], end, err
	}

	return consumed, dest[:produced], EndNone, nil
}
