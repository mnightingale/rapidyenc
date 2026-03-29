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
	state State,
	kernel func(dest, src []byte, escFirst uint64, nextMask uint16) (int, int, uint64, uint16),
) (nSrc int, decoded []byte, pState State, end End, err error) {
	const dotUnstuffing = true
	const searchEnd = true
	length := len(src)

	if length <= width*2 {
		return decodeGeneric(dest, src, state)
	}

	consumed := 0
	produced := 0

	if alignment := width - int(uintptr(unsafe.Pointer(&src[0]))&uintptr(width-1)); alignment != width {
		length -= alignment
		nSrc, decoded, state, end, err = decodeGeneric(dest, src[:alignment], state)
		if end != EndNone {
			return nSrc, decoded, state, end, err
		}
		consumed += nSrc
		produced += len(decoded)
		src = src[nSrc:]
	}

	lenBuffer := width - 1
	if searchEnd {
		lenBuffer += 3
		if dotUnstuffing {
			lenBuffer += 1
		}
	} else if dotUnstuffing {
		lenBuffer += 3
	}

	if length > lenBuffer {
		// Core SIMD logic
		var nextMask uint16

		switch state {
		case StateCRLF:
			if dotUnstuffing && src[0] == '.' {
				nextMask = 1
				if searchEnd && bytes.Equal(src[1:], []byte("\r\n")) {
					state = StateCRLF
					return 3, dest[:0], state, EndArticle, nil
				}
				if searchEnd && bytes.Equal(src[1:], []byte("=y")) {
					state = StateNone
					return 3, dest[:0], state, EndControl, nil
				}
			} else if searchEnd && bytes.Equal(src, []byte("=y")) {
				state = StateNone
				return 2, dest[:0], state, EndControl, nil
			}
		case StateCR:
			if dotUnstuffing && len(src) >= 2 && src[0] == '\n' && src[1] == '.' {
				nextMask = 2
				if searchEnd && bytes.Equal(src[2:], []byte("\r\n")) {
					state = StateCRLF
					return 4, dest[:0], state, EndArticle, nil
				}
				if searchEnd && bytes.Equal(src[2:], []byte("=y")) {
					state = StateNone
					return 4, dest[:0], state, EndControl, nil
				}
			} else if searchEnd && bytes.Equal(src[2:], []byte("\n=y")) {
				state = StateNone
				return 3, dest[:0], state, EndControl, nil
			}
		case StateCRLFDT:
			if searchEnd && bytes.Equal(src, []byte("\r\n")) {
				state = StateCRLF
				return 2, dest[:0], state, EndArticle, nil
			}
			if searchEnd && bytes.Equal(src, []byte("=y")) {
				state = StateNone
				return 2, dest[:0], state, EndControl, nil
			}
		case StateCRLFDTCR:
			if searchEnd && bytes.Equal(src, []byte("\n")) {
				state = StateCRLF
				return 1, dest[:0], state, EndArticle, nil
			}
		case StateCRLFEQ:
			if searchEnd && bytes.Equal(src, []byte("y")) {
				state = StateNone
				return 1, dest[:0], state, EndControl, nil
			}
		}

		var escFirst uint64
		if state == StateEQ || state == StateCRLFEQ {
			escFirst = 1
		}

		c, p, escFirst, nextMask := kernel(dest[produced:], src, escFirst, nextMask)
		consumed += c
		produced += p
		src = src[c:]
		length -= c

		switch {
		case escFirst > 0:
			state = StateEQ
		case nextMask == 1:
			state = StateCRLF
		case nextMask == 2:
			state = StateCR
		default:
			state = StateNone
		}
	}

	if length > 0 {
		nSrc, decoded, state, end, err = decodeGeneric(dest[produced:], src, state)
		consumed += nSrc
		produced += len(decoded)
		return consumed, dest[:produced], state, end, err
	}

	return consumed, dest[:produced], state, EndNone, nil
}
