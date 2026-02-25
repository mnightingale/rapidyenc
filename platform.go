//go:build !cgo

package rapidyenc

import (
	"fmt"
)

var version = 0x999999

// Version returns the version of the backing rapidyenc library.
func Version() string {
	return fmt.Sprintf("%d.%d.%d", version>>16&0xff, version>>8&0xff, version&0xff)
}

// DecodeKernel returns the name of the implementation being used for decode operations
func DecodeKernel() string {
	return "generic"
}

// EncodeKernel returns the name of the implementation being used for encode operations
func EncodeKernel() string {
	return "generic"
}
