//go:build !cgo && goexperiment.simd

package rapidyenc

import (
	"simd/archsimd"
	"unsafe"
)

var (
	compactLUT [32768][16]byte
)

func init() {
	const tableSize = 16
	for i := range compactLUT {
		k := i
		p := 0
		for j := range tableSize {
			if (k & 1) == 0 {
				compactLUT[i][p] = byte(j)
				p++
			}
			k >>= 1
		}
		for ; p < tableSize; p++ {
			compactLUT[i][p] = 0x80
		}
	}

	decoderKernel = "NEON"
}

func decodeIncremental(dest, src []byte, state State) (nSrc int, decoded []byte, pState State, end End, err error) {
	return decodeSIMD(64, dest, src, state, decodeNEON)
}

var (
	specialLut                                                                     archsimd.Uint8x16
	broadcastEscapeFirst, broadcast42, broadcastNeg106                             archsimd.Uint8x16
	broadcastDOT, broadcastZERO, broadcastEQ, broadcastCR, broadcastLF, broadcastY archsimd.Uint8x16
	broadcastEQY                                                                   archsimd.Uint16x8
	broadcastHiByte                                                                archsimd.Uint16x8
	nextMaskMix1, nextMaskMix2                                                     archsimd.Uint8x16
	permuteBitMask                                                                 archsimd.Uint8x16
	broadcastFF                                                                    archsimd.Uint8x16
	unescapeLut                                                                    archsimd.Uint8x16
)

func init() {
	broadcastEscapeFirst = archsimd.LoadUint8x16([]byte{
		42 + 64, 42, 42, 42, 42, 42, 42, 42, 42, 42, 42, 42, 42, 42, 42, 42,
	})

	// search for special chars
	specialLut = archsimd.LoadUint8x16([]byte{
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 255, 0, 0, 255, 0, 0,
	})

	nextMaskMix1 = archsimd.LoadUint8x16([]byte{
		1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	})
	nextMaskMix2 = archsimd.LoadUint8x16([]byte{
		0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	})
	permuteBitMask = archsimd.LoadUint8x16([]byte{1, 2, 4, 8, 16, 32, 64, 128, 1, 2, 4, 8, 16, 32, 64, 128})
	unescapeLut = archsimd.LoadUint8x16([]byte{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1})
	broadcastDOT = archsimd.BroadcastUint8x16('.')
	broadcastZERO = archsimd.BroadcastUint8x16(0)
	broadcastEQ = archsimd.BroadcastUint8x16('=')
	broadcast42 = archsimd.BroadcastUint8x16(42)
	broadcastCR = archsimd.BroadcastUint8x16('\r')
	broadcastLF = archsimd.BroadcastUint8x16('\n')
	broadcastEQY = archsimd.BroadcastUint16x8(0x793d)
	broadcastY = archsimd.BroadcastUint8x16('y')
	broadcastNeg106 = archsimd.BroadcastUint8x16(42 + 64)
	broadcastFF = archsimd.BroadcastUint8x16(0xff)
	broadcastHiByte = archsimd.BroadcastUint16x8(0xff00)
}

// must match the constants decodeSIMD is compiled with
const (
	isRaw     = true
	searchEnd = true
)

func decodeNEON(dest, src []byte, escFirst uint64, nextMask uint16) (int, int, uint64, uint16) {
	if len(dest) < len(src) {
		panic("slice y is shorter than slice x")
	}

	consumed := 0
	produced := 0

	dest = dest[:len(src)]

	var yencOffset archsimd.Uint8x16
	if escFirst > 0 {
		yencOffset = broadcastEscapeFirst
	} else {
		yencOffset = broadcast42
	}
	nextMaskMix := broadcastZERO
	if isRaw {
		switch nextMask {
		case 1:
			nextMaskMix = nextMaskMix1
		case 2:
			nextMaskMix = nextMaskMix2
		}
	}

	// bytes the loop reads past the end of each block
	readAhead := 4
	if searchEnd {
		readAhead = 16
	}

	n := len(src)

	// nextMask for the scalar tail; the loop overwrites it if it bails out early
	blocks := 0
	if n >= 64+readAhead {
		blocks = (n - readAhead) / 64
	}
	if !isRaw {
		nextMask = 0
	} else if blocks > 0 {
		e := 64 * blocks
		switch {
		case src[e-2] == '\r' && src[e-1] == '\n' && src[e] == '.':
			nextMask = 1
		case src[e-1] == '\r' && src[e] == '\n' && src[e+1] == '.':
			nextMask = 2
		default:
			nextMask = 0
		}
	}

	srcP := unsafe.Pointer(&src[0])
	destP := unsafe.Pointer(&dest[0])
	for ; consumed+64+readAhead <= n; consumed += 64 {
		s := unsafe.Add(srcP, consumed)
		d := unsafe.Add(destP, produced)
		oDataA, oDataB, oDataC, oDataD := vld1q_u8_x4(s)

		// search for special chars
		cmpEqA := oDataA.Equal(broadcastEQ).ToInt8x16().ToBits()
		cmpEqB := oDataB.Equal(broadcastEQ).ToInt8x16().ToBits()
		cmpEqC := oDataC.Equal(broadcastEQ).ToInt8x16().ToBits()
		cmpEqD := oDataD.Equal(broadcastEQ).ToInt8x16().ToBits()
		cmpA := specialChars(cmpEqA, oDataA)
		cmpB := specialChars(cmpEqB, oDataB)
		cmpC := specialChars(cmpEqC, oDataC)
		cmpD := specialChars(cmpEqD, oDataD)

		if isRaw {
			cmpA = cmpA.Or(nextMaskMix)
		}

		if !neonVectIsNonzero(cmpA.Or(cmpB).Or(cmpC).Or(cmpD)) {
			oDataA = oDataA.Sub(yencOffset)
			oDataB = oDataB.Sub(broadcast42)
			oDataC = oDataC.Sub(broadcast42)
			oDataD = oDataD.Sub(broadcast42)
			oDataA.StoreArray((*[16]uint8)(d))
			oDataB.StoreArray((*[16]uint8)(unsafe.Add(d, 16)))
			oDataC.StoreArray((*[16]uint8)(unsafe.Add(d, 32)))
			oDataD.StoreArray((*[16]uint8)(unsafe.Add(d, 48)))
			produced += 64
			escFirst = 0
			yencOffset = broadcast42
		} else {
			cmpMerge := addPairs(addPairs(cmpA.And(permuteBitMask), cmpB.And(permuteBitMask)), addPairs(cmpC.And(permuteBitMask), cmpD.And(permuteBitMask)))
			cmpEqMerge := addPairs(addPairs(cmpEqA.And(permuteBitMask), cmpEqB.And(permuteBitMask)), addPairs(cmpEqC.And(permuteBitMask), cmpEqD.And(permuteBitMask)))
			cmpCombined := addPairs(cmpMerge, cmpEqMerge)
			re := cmpCombined.ReshapeToUint64s()
			mask := re.GetElem(0)
			maskEq := re.GetElem(1)

			var match2EqA archsimd.Uint8x16
			var match2EqB archsimd.Uint8x16
			var match2EqC archsimd.Uint8x16
			var match2EqD archsimd.Uint8x16
			var match2CrXDtA archsimd.Uint8x16
			var match2CrXDtB archsimd.Uint8x16
			var match2CrXDtC archsimd.Uint8x16
			var match2CrXDtD archsimd.Uint8x16

			// handle \r\n. sequences
			// RFC3977 requires the first dot on a line to be stripped, due to dot-stuffing
			if (isRaw || searchEnd) && mask != maskEq {
				var tmpData2, nextData archsimd.Uint8x16
				if isRaw && !searchEnd {
					tmpData2 = archsimd.LoadUint8x16Array((*[16]uint8)(unsafe.Add(s, 2+16*3)))
				} else {
					nextData = archsimd.LoadUint8x16Array((*[16]uint8)(unsafe.Add(s, 16*4)))
					tmpData2 = nextData.ConcatShiftBytesRight(oDataD, 2)
				}

				cmpCrA := oDataA.Equal(broadcastCR).ToInt8x16().ToBits()
				cmpCrB := oDataB.Equal(broadcastCR).ToInt8x16().ToBits()
				cmpCrC := oDataC.Equal(broadcastCR).ToInt8x16().ToBits()
				cmpCrD := oDataD.Equal(broadcastCR).ToInt8x16().ToBits()

				if searchEnd {
					match2EqD = tmpData2.Equal(broadcastEQ).ToInt8x16().ToBits()
				}
				if isRaw {
					match2CrXDtA = cmpCrA.And(oDataB.ConcatShiftBytesRight(oDataA, 2).Equal(broadcastDOT).ToInt8x16().ToBits())
					match2CrXDtB = cmpCrB.And(oDataC.ConcatShiftBytesRight(oDataB, 2).Equal(broadcastDOT).ToInt8x16().ToBits())
					match2CrXDtC = cmpCrC.And(oDataD.ConcatShiftBytesRight(oDataC, 2).Equal(broadcastDOT).ToInt8x16().ToBits())
					match2CrXDtD = cmpCrD.And(tmpData2.Equal(broadcastDOT).ToInt8x16().ToBits())
				}

				// find patterns of \r_.
				if isRaw && neonVectIsNonzero(match2CrXDtA.Or(match2CrXDtB).Or(match2CrXDtC).Or(match2CrXDtD)) {
					// merge matches for \r\n.
					match1LfA := oDataB.ConcatShiftBytesRight(oDataA, 1).Equal(broadcastLF).ToInt8x16().ToBits()
					match1LfB := oDataC.ConcatShiftBytesRight(oDataB, 1).Equal(broadcastLF).ToInt8x16().ToBits()
					match1LfC := oDataD.ConcatShiftBytesRight(oDataC, 1).Equal(broadcastLF).ToInt8x16().ToBits()
					var match1LfD archsimd.Uint8x16
					if searchEnd {
						match1LfD = nextData.ConcatShiftBytesRight(oDataD, 1).Equal(broadcastLF).ToInt8x16().ToBits()
					} else {
						match1LfD = archsimd.LoadUint8x16Array((*[16]uint8)(unsafe.Add(s, 1+16*3))).Equal(broadcastLF).ToInt8x16().ToBits()
					}
					// merge matches of \r_. with those for \n
					match2NlDotA := match2CrXDtA.And(match1LfA)
					match2NlDotB := match2CrXDtB.And(match1LfB)
					match2NlDotC := match2CrXDtC.And(match1LfC)
					match2NlDotD := match2CrXDtD.And(match1LfD)
					if searchEnd {
						match1NlA := match1LfA.And(cmpCrA)
						match1NlB := match1LfB.And(cmpCrB)
						match1NlC := match1LfC.And(cmpCrC)
						match1NlD := match1LfD.And(cmpCrD)

						tmpData3 := nextData.ConcatShiftBytesRight(oDataD, 3)
						tmpData4 := nextData.ConcatShiftBytesRight(oDataD, 4)
						// match instances of \r\n.\r\n and \r\n.=y
						match3CrD := tmpData3.Equal(broadcastCR).ToInt8x16().ToBits()
						match4LfD := tmpData4.Equal(broadcastLF).ToInt8x16().ToBits()
						match4Nl := mergeCompares(
							match1NlB.ConcatShiftBytesRight(match1NlA, 3),
							match1NlC.ConcatShiftBytesRight(match1NlB, 3),
							match1NlD.ConcatShiftBytesRight(match1NlC, 3),
							match3CrD.And(match4LfD),
						)
						match4EqY := mergeCompares(
							// match with =y
							eqY16(oDataB.ConcatShiftBytesRight(oDataA, 4)),
							eqY16(oDataC.ConcatShiftBytesRight(oDataB, 4)),
							eqY16(oDataD.ConcatShiftBytesRight(oDataC, 4)),
							eqY16(tmpData4),
						)
						match2EqA = cmpEqB.ConcatShiftBytesRight(cmpEqA, 2)
						match2EqB = cmpEqC.ConcatShiftBytesRight(cmpEqB, 2)
						match2EqC = cmpEqD.ConcatShiftBytesRight(cmpEqC, 2)
						match3EqY := mergeCompares(
							oDataB.ConcatShiftBytesRight(oDataA, 3).Equal(broadcastY).ToInt8x16().ToBits().And(match2EqA),
							oDataC.ConcatShiftBytesRight(oDataB, 3).Equal(broadcastY).ToInt8x16().ToBits().And(match2EqB),
							oDataD.ConcatShiftBytesRight(oDataC, 3).Equal(broadcastY).ToInt8x16().ToBits().And(match2EqC),
							tmpData3.Equal(broadcastY).ToInt8x16().ToBits().And(match2EqD),
						)

						// merge \r\n and =y matches for tmpData4
						match4End := match4Nl.Or(vsriq_n_u16_8(match4EqY, match3EqY))
						// merge with \r\n.
						match2NlDot := mergeCompares(match2NlDotA, match2NlDotB, match2NlDotC, match2NlDotD)
						match4End = match4End.And(match2NlDot)
						// match \r\n=y
						match1Nl := mergeCompares(match1NlA, match1NlB, match1NlC, match1NlD)
						match3End := match3EqY.And(match1Nl)
						// combine match sequences
						if neonVectIsNonzero(match4End.Or(match3End)) {
							// terminator found
							// there's probably faster ways to do this, but reverting to scalar code should be good enough
							nextMask = setNextMask(s, mask)
							break
						}
					}
					match2NlDotDMasked := match2NlDotD.And(permuteBitMask)
					mergeKillDots := addPairs(addPairs(match2NlDotA.And(permuteBitMask), match2NlDotB.And(permuteBitMask)), addPairs(match2NlDotC.And(permuteBitMask), match2NlDotDMasked))
					mergeKillDots = addPairs(mergeKillDots, mergeKillDots)
					mergeKillDotsShifted := mergeKillDots.ReshapeToUint64s().ShiftAllLeft(2)
					mask |= mergeKillDotsShifted.GetElem(0)
					cmpCombined = cmpCombined.Or(mergeKillDotsShifted.ReshapeToUint8s())
					nextMaskMix = broadcastZERO.ConcatShiftBytesRight(match2NlDotD, 14)
				} else if searchEnd {
					match2EqA = cmpEqB.ConcatShiftBytesRight(cmpEqA, 2)
					match2EqB = cmpEqC.ConcatShiftBytesRight(cmpEqB, 2)
					match2EqC = cmpEqD.ConcatShiftBytesRight(cmpEqC, 2)

					match3EqYA := match2EqA.And(oDataB.ConcatShiftBytesRight(oDataA, 3).Equal(broadcastY).ToInt8x16().ToBits())
					match3EqYB := match2EqB.And(oDataC.ConcatShiftBytesRight(oDataB, 3).Equal(broadcastY).ToInt8x16().ToBits())
					match3EqYC := match2EqC.And(oDataD.ConcatShiftBytesRight(oDataC, 3).Equal(broadcastY).ToInt8x16().ToBits())
					match3EqYD := match2EqD.And(nextData.ConcatShiftBytesRight(oDataD, 3).Equal(broadcastY).ToInt8x16().ToBits())

					if neonVectIsNonzero(match3EqYA.Or(match3EqYB).Or(match3EqYC).Or(match3EqYD)) {
						match1LfA := oDataB.ConcatShiftBytesRight(oDataA, 1).Equal(broadcastLF).ToInt8x16().ToBits()
						match1LfB := oDataC.ConcatShiftBytesRight(oDataB, 1).Equal(broadcastLF).ToInt8x16().ToBits()
						match1LfC := oDataD.ConcatShiftBytesRight(oDataC, 1).Equal(broadcastLF).ToInt8x16().ToBits()
						match1LfD := nextData.ConcatShiftBytesRight(oDataD, 1).Equal(broadcastLF).ToInt8x16().ToBits()
						matchEnd := match3EqYA.And(match1LfA.And(cmpCrA)).
							Or(match3EqYB.And(match1LfB.And(cmpCrB))).
							Or(match3EqYC.And(match1LfC.And(cmpCrC)).
								Or(match3EqYD.And(match1LfD.And(cmpCrD))))
						if neonVectIsNonzero(matchEnd) {
							nextMask = setNextMask(s, mask)
							break
						}
					}
					if isRaw {
						nextMaskMix = broadcastZERO
					}
				} else if isRaw {
					nextMaskMix = broadcastZERO
				}
			}

			// a spec compliant encoder should never generate sequences: ==, =\n and =\r, but we'll handle them to be spec compliant
			// the yEnc specification requires any character following = to be unescaped, not skipped over, so we'll deal with that
			// firstly, check for invalid sequences of = (we assume that these are rare, as a spec compliant yEnc encoder should not generate these)
			maskEqShift1 := (maskEq << 1) | escFirst
			if mask&maskEqShift1 != 0 {
				maskEq = fixEqMask(maskEq, maskEqShift1)
				nextEscFirst := maskEq >> 63
				// next, eliminate anything following a `=` from the special char mask; this eliminates cases of `=\r` so that they aren't removed
				maskEq = (maskEq << 1) | escFirst
				mask &= ^maskEq
				escFirst = nextEscFirst

				// unescape chars following `=`
				{
					maskEqTemp := archsimd.BroadcastUint64x2(maskEq).ReshapeToUint8s()
					cmpCombined = cmpCombined.AndNot(maskEqTemp) // `mask &= ~maskEq` in vector form

					vMaskEqA := maskEqTemp.LookupOrZero(unescapeLut)
					maskEqTemp = maskEqTemp.ConcatShiftBytesRight(maskEqTemp, 2)
					vMaskEqB := maskEqTemp.LookupOrZero(unescapeLut)
					maskEqTemp = maskEqTemp.ConcatShiftBytesRight(maskEqTemp, 2)
					vMaskEqC := maskEqTemp.LookupOrZero(unescapeLut)
					maskEqTemp = maskEqTemp.ConcatShiftBytesRight(maskEqTemp, 2)
					vMaskEqD := maskEqTemp.LookupOrZero(unescapeLut)

					oDataA = oDataA.Sub(broadcastNeg106.IfElse(vtstq_u8(vMaskEqA, permuteBitMask), broadcast42))
					oDataB = oDataB.Sub(broadcastNeg106.IfElse(vtstq_u8(vMaskEqB, permuteBitMask), broadcast42))
					oDataC = oDataC.Sub(broadcastNeg106.IfElse(vtstq_u8(vMaskEqC, permuteBitMask), broadcast42))
					oDataD = oDataD.Sub(broadcastNeg106.IfElse(vtstq_u8(vMaskEqD, permuteBitMask), broadcast42))
				}
			} else {
				// no invalid = sequences found - we can cut out some things from above
				// this code path is a shortened version of above; it's here because it's faster, and what we'll be dealing with most of the time
				escFirst = maskEq >> 63

				oDataA = oDataA.Sub(broadcastNeg106.IfElse(cmpEqA.ConcatShiftBytesRight(broadcast42, 15).Equal(broadcastFF), yencOffset))
				oDataB = oDataB.Sub(broadcastNeg106.IfElse(cmpEqB.ConcatShiftBytesRight(cmpEqA, 15).Equal(broadcastFF), broadcast42))
				oDataC = oDataC.Sub(broadcastNeg106.IfElse(cmpEqC.ConcatShiftBytesRight(cmpEqB, 15).Equal(broadcastFF), broadcast42))
				oDataD = oDataD.Sub(broadcastNeg106.IfElse(cmpEqD.ConcatShiftBytesRight(cmpEqC, 15).Equal(broadcastFF), broadcast42))
			}

			yencOffset = yencOffset.SetElem(0, uint8((escFirst<<6)|42))

			{
				//// all that's left is to 'compress' the data (skip over masked chars)
				counts := 0x0808080808080808 - cmpCombined.OnesCount().ReshapeToUint64s().GetElem(0)
				counts += counts >> 8

				countA := int(counts & 0xff)
				countB := countA + int((counts>>16)&0xff)
				countC := countB + int((counts>>32)&0xff)
				countD := countC + int((counts>>48)&0xff)

				oDataA.LookupOrZero(archsimd.LoadUint8x16Array(&compactLUT[mask&0x7fff])).StoreArray((*[16]uint8)(d))
				oDataB.LookupOrZero(archsimd.LoadUint8x16Array(&compactLUT[(mask>>16)&0x7fff])).StoreArray((*[16]uint8)(unsafe.Add(d, countA)))
				oDataC.LookupOrZero(archsimd.LoadUint8x16Array(&compactLUT[(mask>>32)&0x7fff])).StoreArray((*[16]uint8)(unsafe.Add(d, countB)))
				oDataD.LookupOrZero(archsimd.LoadUint8x16Array(&compactLUT[(mask>>48)&0x7fff])).StoreArray((*[16]uint8)(unsafe.Add(d, countC)))
				produced += countD
			}
		}
	}
	return consumed, produced, escFirst, nextMask
}

// setNextMask mirrors decoder_set_nextMask (no backtracking)
func setNextMask(s unsafe.Pointer, mask uint64) uint16 {
	if isRaw {
		if *(*uint8)(s) == '.' {
			return uint16(mask) & 1
		}
		if *(*uint8)(unsafe.Add(s, 1)) == '.' {
			return uint16(mask) & 2
		}
	}
	return 0
}

func neonVectIsNonzero(v archsimd.Uint8x16) bool {
	return 0 != v.ReshapeToUint64s().SaturateToUint32().ReshapeToUint64s().GetElem(0)
}

// specialChars marks \n, \r and = within data.
func specialChars(cmpEq, data archsimd.Uint8x16) archsimd.Uint8x16 {
	return specialLut.LookupOrZero(data).Or(cmpEq)
}

// mergeCompares mixes four comparison vectors into one so a single non-zero
// test covers all four. Each source owns disjoint bit positions, so And/Or of
// two merged vectors equals the merge of the per-source And/Or.
// constant vectors arbitrarily chosen from ones that can be reused; exact
// ordering of bits doesn't matter, we just need to mix them in
func mergeCompares(a, b, c, d archsimd.Uint8x16) archsimd.Uint8x16 {
	return vbslq_u8(broadcastEQ, vbslq_u8(broadcastY, a, b), vbslq_u8(broadcastY, c, d))
}

func vld1q_u8_x4(s unsafe.Pointer) (archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16) {
	oDataA := archsimd.LoadUint8x16Array((*[16]uint8)(s))
	oDataB := archsimd.LoadUint8x16Array((*[16]uint8)(unsafe.Add(s, 16)))
	oDataC := archsimd.LoadUint8x16Array((*[16]uint8)(unsafe.Add(s, 32)))
	oDataD := archsimd.LoadUint8x16Array((*[16]uint8)(unsafe.Add(s, 48)))
	return oDataA, oDataB, oDataC, oDataD
}

// eqY16 matches "=y" at even offsets, as a byte vector.
func eqY16(data archsimd.Uint8x16) archsimd.Uint8x16 {
	return data.ReshapeToUint16s().Equal(broadcastEQY).ToInt16x8().ToBits().ReshapeToUint8s()
}

// vbslq_u8 emulates BSL
func vbslq_u8(mask, a, b archsimd.Uint8x16) archsimd.Uint8x16 {
	return a.And(mask).Or(b.AndNot(mask))
}

// vsriq_n_u16_8 emulates SRI #8: keeps the high byte of each 16-bit lane of a
// and inserts the high byte of b as the low byte
func vsriq_n_u16_8(a, b archsimd.Uint8x16) archsimd.Uint8x16 {
	return a.ReshapeToUint16s().And(broadcastHiByte).Or(b.ReshapeToUint16s().ShiftAllRight(8)).ReshapeToUint8s()
}

// vtstq_u8 emulates CMTST
func vtstq_u8(a, b archsimd.Uint8x16) archsimd.Mask8x16 {
	return a.And(b).NotEqual(broadcastZERO)
}

// addPairs emulates ADDP
func addPairs(a, b archsimd.Uint8x16) archsimd.Uint8x16 {
	return a.ConcatEven(b).Add(a.ConcatOdd(b))
}

// resolve invalid sequences of = to deal with cases like '===='
// bit hack inspired from simdjson: https://youtu.be/wlvKAT7SZIQ?t=33m38s
func fixEqMask(mask, maskShift1 uint64) uint64 {
	// isolate the start of each consecutive bit group (e.g. 01011101 -> 01000101)
	start := mask & ^maskShift1

	// this strategy works by firstly separating groups that start on even/odd bits
	// generally, it doesn't matter which one (even/odd) we pick, but clearing even groups specifically allows the escFirst bit in maskShift1 to work
	// (this is because the start of the escFirst group is at index -1, an odd bit, but we can't clear it due to being < 0, so we just retain all odd groups instead)

	even := uint64(0x5555555555555555) // every even bit (01010101...)

	// obtain groups which start on an odd bit (clear groups that start on an even bit, but this leaves an unwanted trailing bit)
	oddGroups := mask + (start & even)

	// clear even bits in odd groups, whilst conversely preserving even bits in even groups
	// the `& mask` also conveniently gets rid of unwanted trailing bits
	return (oddGroups ^ even) & mask
}
