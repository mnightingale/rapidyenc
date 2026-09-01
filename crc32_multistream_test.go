package rapidyenc

import (
	"hash/crc32"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

// crcUpdate must be indistinguishable from crc32.Update at every length,
// especially around the chunk boundary where the multi-stream path engages.
func TestCRCUpdateMatchesStdlib(t *testing.T) {
	rnd := rand.New(rand.NewSource(3))
	buf := make([]byte, crcChunkSize*3+2048)
	rnd.Read(buf)

	lengths := []int{0, 1, 63, 64, 65, 1023,
		crcChunkSize - 1, crcChunkSize, crcChunkSize + 1,
		crcChunkSize + 63, crcChunkSize * 2, crcChunkSize*2 + 777,
		crcChunkSize*3 + 2048}

	for _, n := range lengths {
		for _, init := range []uint32{0, 0xffffffff, 0x12345678} {
			require.Equal(t,
				crc32.Update(init, crc32.IEEETable, buf[:n]),
				crcUpdate(init, buf[:n]),
				"len=%d init=%#08x", n, init)
		}
	}
}

// Splitting a buffer across calls must give the same answer as one call.
func TestCRCUpdateIncremental(t *testing.T) {
	rnd := rand.New(rand.NewSource(5))
	buf := make([]byte, crcChunkSize*2+1234)
	rnd.Read(buf)

	want := crc32.ChecksumIEEE(buf)
	for _, split := range []int{1, 100, crcQuarterBig, crcChunkSize, crcChunkSize + 7} {
		var got uint32
		for i := 0; i < len(buf); i += split {
			got = crcUpdate(got, buf[i:min(i+split, len(buf))])
		}
		require.Equal(t, want, got, "split=%d", split)
	}
}

func TestCRCCombineHelpers(t *testing.T) {
	rnd := rand.New(rand.NewSource(11))
	for _, n := range []int{8, 64, 256, 1024, crcQuarterSml, crcQuarterBig} {
		k := shiftZeros(0x80000000, n)
		for i := 0; i < 8; i++ {
			a := rnd.Uint32()
			require.Equal(t, shiftZeros(a, n), crcMul(a, k), "n=%d a=%#08x", n, a)
		}
	}
}
