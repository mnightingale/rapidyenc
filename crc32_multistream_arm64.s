//go:build !cgo

#include "textflag.h"

// func crc32IEEEQuad(crcs *[4]uint32, p []byte)
//
// Advances four non-inverted CRC32 registers over four equal quarters of p.
// len(p) must be a multiple of 64 so each quarter is a multiple of 16.
//
// The four streams are independent, so the CRC32X instructions issue back to
// back instead of stalling on each other's latency.
TEXT ·crc32IEEEQuad(SB),NOSPLIT,$0-32
	MOVD	crcs+0(FP), R0
	MOVD	p+8(FP), R13
	MOVD	p_len+16(FP), R20
	LSR	$2, R20, R20		// bytes per stream
	ADD	R13, R20, R14
	ADD	R14, R20, R15
	ADD	R15, R20, R16

	MOVWU	(R0), R9
	MOVWU	4(R0), R10
	MOVWU	8(R0), R11
	MOVWU	12(R0), R12

loop:
	CBZ	R20, done
	LDP.P	16(R13), (R1, R2)
	LDP.P	16(R14), (R3, R4)
	LDP.P	16(R15), (R5, R6)
	LDP.P	16(R16), (R7, R8)
	CRC32X	R1, R9
	CRC32X	R3, R10
	CRC32X	R5, R11
	CRC32X	R7, R12
	CRC32X	R2, R9
	CRC32X	R4, R10
	CRC32X	R6, R11
	CRC32X	R8, R12
	SUB	$16, R20
	JMP	loop

done:
	MOVW	R9, (R0)
	MOVW	R10, 4(R0)
	MOVW	R11, 8(R0)
	MOVW	R12, 12(R0)
	RET
