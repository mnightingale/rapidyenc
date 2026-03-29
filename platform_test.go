package rapidyenc

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDecodeKernel(t *testing.T) {
	assert.NotEqual(t, DecodeKernel(), "unknown")
}

func TestEncodeKernel(t *testing.T) {
	assert.NotEqual(t, EncodeKernel(), "unknown")
}
