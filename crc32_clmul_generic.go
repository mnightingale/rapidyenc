//go:build !(goexperiment.simd && arm64)

package rapidyenc

// clmul32 returns the carry-less product of a and b.
func clmul32(a, b uint32) uint64 {
	var res uint64
	x := uint64(a)
	for i := 0; i < 32; i++ {
		if b&(1<<uint(i)) != 0 {
			res ^= x << uint(i)
		}
	}
	return res
}
