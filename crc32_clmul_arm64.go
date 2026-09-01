//go:build goexperiment.simd && arm64

package rapidyenc

import "simd/archsimd"

// clmul32 returns the carry-less product of a and b, using PMULL.
func clmul32(a, b uint32) uint64 {
	x := archsimd.BroadcastUint64x2(uint64(a))
	y := archsimd.BroadcastUint64x2(uint64(b))
	return x.CarrylessMultiplyEven(y).GetElem(0)
}
