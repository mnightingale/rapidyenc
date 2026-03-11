//go:build !cgo && !(goexperiment.simd && amd64)

package rapidyenc

// DecodeKernel returns the name of the implementation being used for decode operations
func DecodeKernel() string {
	return "generic"
}

// EncodeKernel returns the name of the implementation being used for encode operations
func EncodeKernel() string {
	return "generic"
}
