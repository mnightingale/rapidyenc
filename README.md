# rapidyenc

**rapidyenc** is a high-performance Go library for decoding [yEnc](https://en.wikipedia.org/wiki/YEnc). It provides fast, memory-efficient decoding with robust error handling, supporting multiple platforms and architectures.

The decoder expects an NNTP stream of data, it will perform dot unstuffing and search for the end of responses ".\r\n" this behaviour is not currently configurable.

The module exposes the highly efficient encoding and decoding implementations provided by the C compatible library [animetosho/rapidyenc](https://github.com/animetosho/rapidyenc) taking advantage CPU features.

## Features

- **Fast yEnc encoding/decoding** using native C implementation via CGO.
- **Streaming interface** for efficient handling of large files.
- **Cross-platform:** Supports Linux, Windows, macOS on `amd64` and `arm64`
- **Header parsing:** Extracts yEnc `Meta` (filename, size, CRC32, etc).
- **Error detection:** CRC mismatch, data corruption, and missing headers.

## Experimental usage without CGO

Experimental support using [simd/archsimd](https://pkg.go.dev/simd/archsimd) is available without the need for CGO, allowing safer more portable usage.

Both the encoder and decoder are ported from the reference implementations:

| Platform          | Kernel                          |
|-------------------|---------------------------------|
| `amd64` with AVX2 | AVX2                            |
| `arm64`           | NEON                            |
| anything else     | generic scalar, and much slower |

Requires Go 1.27, built with:

`CGO_ENABLED=0 GOEXPERIMENT=simd`

`DecodeKernel()` and `EncodeKernel()` report which implementation is in use.

If [simd/archsimd](https://pkg.go.dev/simd/archsimd) is promoted from an experiment I expect CGO usage/support will be removed entirely.

## Encoding

```go
// An io.Reader of raw data, here random data, but could be a file, bufio.Reader, etc.
raw := make([]byte, 768_000)
_, err := rand.Read(raw)
input := bytes.NewReader(raw)

// yEnc headers
meta := Meta{
    FileName:   "filename",
    FileSize:   int64(len(raw)),
    PartSize:   int64(len(raw)),
    PartNumber: 1,
    TotalParts: 1,
}

// io.Writer for output
encoded := bytes.NewBuffer(nil)

// Pass input through the Encoder
enc, err := rapidyenc.NewEncoder(encoded, meta)
_, err = io.Copy(enc, input)

// Must close to write the =yend footer
err = enc.Close()
```

## Decoding

```go
// An io.Reader of yEnc encoded data
encoded := bytes.NewReader(raw)

// Will read from input until io.EOF or ".\r\n"
dec := rapidyenc.NewDecoder(encoded)
response, err := dec.Next()
// if err == nil then response.Data contains the decoded response and response.Metadata yEnc headers, crc, etc.
// response is also returned even when there is an err but response.Data might be nil
```

### Advanced usage

#### WithDataFunc

The above decoding example is suitable for one-off usage however for repeated use it is best to reuse a Decoder instance and output buffers to reduce allocations and garbage collector pressure.

For optimal usage keep a Decoder instance per reader long term and provide it a pool of output buffers via sync.Pool or similar.

```go
bufferPool := sync.Pool{
    New: func() any {
        return make([]byte, 0, 1024*1024) // 1 MiB, choose a size suitable to contain the expected encoded size
    },
}

dec := rapidyenc.NewDecoder(encoded, rapidyenc.WithDataFunc(func() []byte {
    return bufferPool.Get().([]byte)
}))
response, err := dec.Next()
// Determine what to do based on response and err
if response != nil && response.Data != nil {
    // Use response.Data, write it to file, etc.
    // When finished put it back in the pool.
    bufferPool.Put(response.Data)
}
```

#### Expect

`Expect` records that a request has been sent, so its response can be paired with it. Responses come back in the order the requests went out, so the decoder holds the queue itself rather than relying on the caller to keep one in step. The value passed is opaque and comes back as `Response.Request`.

The second argument is where the decoded body should go, or `nil` to collect it into `Response.Data` as usual. An `io.WriterAt` receives the body at the offset the yEnc headers declare, so parts fetched out of order on separate connections each land in the right place:

```go
f, err := os.Create("output.bin")

dec := rapidyenc.NewDecoder(conn, rapidyenc.WithStatusLineAlreadyRead())
if err := dec.Expect(articleID, f); err != nil {
    return err
}
response, err := dec.Next()
// response.Request is articleID, response.Data is nil, the payload is in f
```

Binding the destination to the request rather than picking it from the response matters: `Meta.FileName` is whatever the poster put in the headers, so it is not something to open a file by. Only the offset is taken from the headers.

That the name is not known until a part has been decoded is not a problem for this. Open a file under a name you chose, give it as the sink for every part of the file, and rename it once the file is complete. `Metadata.FileName` is there to inform that decision rather than to make it, and the decoder never acts on it. One `*os.File` can be the sink for several Decoders at once, so parts arriving on different connections write into it concurrently without a lock.

An `io.Writer` receives the body sequentially instead, which is what an `XZVER` or `XZHDR` response wants since it carries no meaningful offset. That is also the case a sink helps most: `Response.Data` sizes itself from the yEnc headers, so a response declaring `size=-1` starts at 1 KiB and doubles, allocating ~2.5x the payload. Starting larger does not help, the last doublings dominate the total. A sink is fed in read-buffer sized chunks, so nothing grows. `BenchmarkUnknownSize` decodes an 8 MiB article with `size=-1` both ways:

```
BenchmarkUnknownSize/Data   2411261 ns/op   3479 MB/s   33523402 B/op   27 allocs/op
BenchmarkUnknownSize/Sink   1578686 ns/op   5314 MB/s        600 B/op   15 allocs/op
```

Three things to know:

- A failed write does not abort the response. The rest of it still has to be read or the connection is left mid-article, so the body is discarded and the failure reported as `Response.SinkFailed` and `Response.SinkError` once the response completes. The article has to be fetched again; the connection does not.
- Bytes reach the sink before they are validated. The size and CRC32 in `=yend` can only be checked once the response ends, so `Next` may return `ErrDataCorruption` or `ErrCrcMismatch` after a whole part has already been written.
- uuencoded responses are never sent to the sink. uu carries no offset for a part to be placed at, so every part would land at offset 0 and overwrite the last. They are buffered into `Response.Data` instead, as `ResponseMeta.Format` reports.

`Pending` is the requests still awaiting a response, oldest first, and `ClearExpected` drops them all for a connection being reset. Both include the response currently arriving, since a request is paired as soon as its first byte shows up and a connection mid-article is not idle.

The request queue is the one part of a Decoder that is safe to use from another goroutine, so a sender can call `Expect` while a receiver is in `Next`, a monitor can call `Pending` to report what a connection is fetching, and a supervisor can call `ClearExpected` to reset one. `Next` itself must be called from one goroutine at a time. A response already being decoded keeps the request and sink it was paired with, so `ClearExpected` does not disturb it.

Without `Expect` nothing changes, so existing callers are unaffected.

#### WithStatusLineAlreadyRead

WithStatusLineAlreadyRead tells the decoder that the NNTP status line has already been consumed from the reader, so it's going to assume that what remains in a multiline response.

```go
rapidyenc.NewDecoder(encoded, rapidyenc.WithStatusLineAlreadyRead())
```

#### WithBufferSize

WithBufferSize controls the Decoder internal buffer (default 32 KiB), the input reader is read into this buffer and decoded into Response.Data  
Larger sizes may be detrimental to performance especially if using pipelining because the response buffer must have at least enough remaining capacity to store the length of the buffer.

```go
rapidyenc.NewDecoder(encoded, rapidyenc.WithBufferSize(64*1024))
```

## Building from Source

It may not be desirable to use the included binary blobs, I could not find a way of avoiding it as there didn't appear to be a way to pass per-file CFLAGS when using CGO. If things have changed or there is a better way please let me know.

See [Makefile](Makefile) and [build.yml](.github/workflows/build.yml) for how the blobs are compiled.

Adding support for other platforms involves creating a `toolchain-*.cmake` file, adjust [Makefile](Makefile), compile and update [cgo.go](cgo.go)

## Contributing

Pull requests and issues are welcome! Please open an issue for bug reports, questions, or feature requests.
