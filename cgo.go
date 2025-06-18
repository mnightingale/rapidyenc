package rapidyenc

/*
#cgo noescape rapidyenc_version
#cgo nocallback rapidyenc_version
#cgo noescape rapidyenc_decode_init
#cgo nocallback rapidyenc_decode_init
#cgo noescape rapidyenc_decode_kernel
#cgo nocallback rapidyenc_decode_kernel
#cgo noescape rapidyenc_decode_incremental_go
#cgo nocallback rapidyenc_decode_incremental_go
#cgo noescape rapidyenc_encode_init
#cgo nocallback rapidyenc_encode_init
#cgo noescape rapidyenc_encode_kernel
#cgo nocallback rapidyenc_encode_kernel
#cgo noescape rapidyenc_encode
#cgo nocallback rapidyenc_encode
#cgo noescape rapidyenc_encode_ex
#cgo nocallback rapidyenc_encode_ex
#cgo darwin LDFLAGS: ${SRCDIR}/librapidyenc_darwin.a -lstdc++
#cgo windows,amd64 LDFLAGS: ${SRCDIR}/librapidyenc_windows_amd64.a -lstdc++
#cgo linux,amd64 LDFLAGS: ${SRCDIR}/librapidyenc_linux_amd64.a -lstdc++
#cgo linux,arm64 LDFLAGS: ${SRCDIR}/librapidyenc_linux_arm64.a -lstdc++
#include "rapidyenc.h"
*/
import "C"
