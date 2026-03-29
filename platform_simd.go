//go:build !cgo && goexperiment.simd && amd64

package rapidyenc

var (
	decoderKernel, encoderKernel string
)

// DecodeKernel returns the name of the implementation being used for decode operations
func DecodeKernel() string {
	return decoderKernel
}

// EncodeKernel returns the name of the implementation being used for encode operations
func EncodeKernel() string {
	return encoderKernel
}
