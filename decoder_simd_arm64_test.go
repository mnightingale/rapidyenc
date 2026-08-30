//go:build !cgo && goexperiment.simd && arm64

package rapidyenc

import (
	"simd/archsimd"
	"testing"
)

// u8 builds a Uint8x16 from the given bytes, zero-padded.
func u8(b ...byte) archsimd.Uint8x16 {
	var a [16]byte
	copy(a[:], b)
	return archsimd.LoadUint8x16Array(&a)
}

func dump(v archsimd.Uint8x16) [16]byte {
	var a [16]byte
	v.StoreArray(&a)
	return a
}

func TestHelpers(t *testing.T) {
	if neonVectIsNonzero(archsimd.BroadcastUint8x16(0)) {
		t.Error("zero reported nonzero")
	}
	for i := 0; i < 16; i++ {
		var a [16]byte
		a[i] = 1
		if !neonVectIsNonzero(archsimd.LoadUint8x16Array(&a)) {
			t.Errorf("byte %d reported zero", i)
		}
	}

	// addPairs == ADDP
	a := u8(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16)
	b := u8(21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36)
	got := dump(addPairs(a, b))
	want := [16]byte{3, 7, 11, 15, 19, 23, 27, 31, 43, 47, 51, 55, 59, 63, 67, 71}
	if got != want {
		t.Errorf("addPairs = %v want %v", got, want)
	}

	// vbslq_u8
	got = dump(vbslq_u8(archsimd.BroadcastUint8x16(0x3d), archsimd.BroadcastUint8x16(0xff), archsimd.BroadcastUint8x16(0x00)))
	if got[0] != 0x3d {
		t.Errorf("vbslq = %#x want 0x3d", got[0])
	}

	// mergeCompares: each source must own at least one bit
	ff := archsimd.BroadcastUint8x16(0xff)
	zero := archsimd.BroadcastUint8x16(0)
	for i, m := range [][4]archsimd.Uint8x16{
		{ff, zero, zero, zero}, {zero, ff, zero, zero},
		{zero, zero, ff, zero}, {zero, zero, zero, ff},
	} {
		if !neonVectIsNonzero(mergeCompares(m[0], m[1], m[2], m[3])) {
			t.Errorf("mergeCompares dropped source %d", i)
		}
	}
	if neonVectIsNonzero(mergeCompares(zero, zero, zero, zero)) {
		t.Error("mergeCompares invented bits")
	}

	// specialChars marks \n, \r and =
	data := u8('a', '\n', 'b', '\r', 'c', '=', 'd', 0, 1, 15, 16, 61, 10, 13, 'z', 'q')
	cmpEq := data.Equal(archsimd.BroadcastUint8x16('=')).ToInt8x16().ToBits()
	got = dump(specialChars(cmpEq, data))
	want = [16]byte{0, 0xff, 0, 0xff, 0, 0xff, 0, 0, 0, 0, 0, 0xff, 0xff, 0xff, 0, 0}
	if got != want {
		t.Errorf("specialChars = %v want %v", got, want)
	}

	// vtstq_u8
	got = dump(archsimd.BroadcastUint8x16(0xaa).IfElse(vtstq_u8(u8(1, 2, 4, 8, 0, 0, 0, 0), permuteBitMask), archsimd.BroadcastUint8x16(0x11)))
	want = [16]byte{0xaa, 0xaa, 0xaa, 0xaa, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11, 0x11}
	if got != want {
		t.Errorf("vtstq_u8 = %v want %v", got, want)
	}

	// vsriq_n_u16_8: hi byte of a kept, hi byte of b becomes lo byte
	got = dump(vsriq_n_u16_8(u8(0x11, 0x22, 0x33, 0x44), u8(0xaa, 0xbb, 0xcc, 0xdd)))
	if got[0] != 0xbb || got[1] != 0x22 || got[2] != 0xdd || got[3] != 0x44 {
		t.Errorf("vsriq_n_u16_8 = %v", got[:4])
	}
}
