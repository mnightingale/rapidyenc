//go:build !cgo

package rapidyenc

import (
	"encoding/binary"
)

func escape(n int) byte {
	if n == 214 || n == '\r'+214 || n == '\n'+214 || n == '='-42 {
		return 0
	} else {
		return byte((n + 42) & 0xff)
	}
}

func escaped(n int) uint16 {
	if n == 214 || n == 214+'\r' || n == 214+'\n' || n == '='-42 || n == 214+'\t' || n == 214+' ' || n == '.'-42 {
		return uint16('=') | (uint16(n+42+64&0xff) << 8)
	} else {
		return 0
	}
}

func init() {
	for i := 0; i < 256; i++ {
		escapeLUT[i] = escape(i)
		escapedLUT[i] = escaped(i)
	}
}

var escapeLUT [256]byte
var escapedLUT [256]uint16

// rapidyenc_encode_ex
func encodeGeneric(lineSize int, colOffset *int, src []byte, dest []byte, doEnd bool) []byte {
	// check len?

	length := len(src)
	pos := 0
	write := 0
	col := *colOffset

	if col == 0 {
		c := src[pos]
		pos++
		if escaped := escapedLUT[c]; escaped != 0 {
			binary.LittleEndian.PutUint16(dest[write:], escapedLUT[c])
			write += 2
			col = 2
		} else {
			dest[write] = c + 42
			write++
			col = 1
		}
	}

	for pos < length {
		sp := 0
		for pos+8 < length && lineSize-col-1 > 8 {
			sp = write
			// C++ unrolls this...
			for n := range 8 {
				c := src[pos+n]
				if escaped := escapeLUT[c]; escaped != 0 {
					dest[write] = escaped
					write++
				} else {
					binary.LittleEndian.PutUint16(dest[write:], escapedLUT[c])
					write += 2
				}
			}
			pos += 8
			col += write - sp
		}

		if sp > 0 && col >= lineSize-1 {
			col -= write - sp
			write = sp
			pos -= 8
		}

		for col < lineSize-1 {
			c := src[pos]
			pos++
			if escaped := escapeLUT[c]; escaped != 0 {
				dest[write] = escaped
				write++
				col++
			} else {
				binary.LittleEndian.PutUint16(dest[write:], escapedLUT[c])
				write += 2
				col += 2
			}
			if pos >= length {
				goto end
			}
		}

		if col < lineSize {
			c := src[pos]
			pos++
			if escapedLUT[c] != 0 && c != '.'-42 {
				binary.LittleEndian.PutUint16(dest[write:], escapedLUT[c])
				write += 2
			} else {
				dest[write] = c + 42
				write++
			}
		}

		if pos >= length {
			break
		}

		c := src[pos]
		pos++
		if escaped := escapedLUT[c]; escaped != 0 {
			binary.LittleEndian.PutUint32(dest[write:], '\r'|('\n'<<8)|(uint32(escaped)<<16))
			write += 4
			col = 2
		} else {
			// #define UINT32_PACK(a, b, c, d) ((a) | ((b) << 8) | ((c) << 16) | ((d) << 24))
			binary.LittleEndian.PutUint32(dest[write:], '\r'|('\n'<<8)|(uint32(c+42)<<16))
			write += 3
			col = 1
		}
	}

end:
	if doEnd {
		lc := dest[write-1]
		if lc == '\t' || lc == ' ' {
			dest[write-1] = '='
			dest[write] = lc + 64
			write++
			col++
		}
	}

	*colOffset = col
	return dest[:write]
}
