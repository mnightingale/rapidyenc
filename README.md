# rapidyenc

**rapidyenc** is a high-performance, CGO-powered Go library for decoding [yEnc](https://en.wikipedia.org/wiki/YEnc) and (partially) UUencoded binaries, primarily for use with Usenet and NNTP applications. It provides fast, memory-efficient decoding with robust error handling, supporting multiple platforms and architectures.

## Features

- ⚡ **Fast yEnc decoding** using native C implementation via CGO.
- 🧩 **Streaming interface** (`io.Reader`) for large files.
- ♻️ **Decoder pooling** for reduced GC pressure and improved performance.
- 🏗️ **Cross-platform:** Supports Linux, Windows, macOS on `amd64`, `arm64`, `386`, and more. (need more testing for darwin and 386 in general)
- 🔎 **Header parsing:** Extracts yEnc `Meta` (filename, size, CRC32, etc).
- 🛠️ **Debugging:** Optional detailed debug output and segment tracking.
- 🛡️ **Error detection:** CRC mismatch, data corruption, and missing headers.

## Usage Example

```go
// Example: Running built-in rapidyenc decoder self-test and file tests

func dlog(logthis bool, format string, a ...any) {
	if !logthis {
		return
	}
	log.Printf(format, a...)
} // end dlog

const always bool = true

if testrapidyenc {
	decoder := rapidyenc.AcquireDecoder()
	decoder.SetDebug(true, true)
	segId := "any@thing.net"
	decoder.SetSegmentId(&segId)
	rapidyenc.ReleaseDecoder(decoder)             // release the decoder
	decoder = nil                                 // clear memory
	errs := rapidyenc.TestRapidyencDecoderFiles() // test rapidyenc decoder with files
	if len(errs) != 0 {
		dlog(always, "ERROR testing rapidyenc decoder: %v", errs)
		os.Exit(1)
	}
	dlog(always, "rapidyenc decoder successfully initialized! quitting now...")
	if runProf {
		Prof.StopCPUProfile() // stop cpu profiling
		time.Sleep(time.Second)
	}
	os.Exit(0) // exit after testing rapidyenc decoder
}
```

This snippet demonstrates how to use the rapidyenc decoder's self-test and file-test facilities. The `TestRapidyencDecoderFiles` function will run through sample `.yenc` files and validate decoding, CRCs, and error handling.  
- **AcquireDecoder / ReleaseDecoder** shows the typical lifecycle of a decoder instance.
- **SetDebug** enables detailed debug output.
- **SetSegmentId** sets an identifier for easier debugging when handling multiple segments.
- If any decoding errors are found in the test files, the process exits with an error code.
- If profiling is enabled (via `runProf`), CPU profiling is stopped before exit.

---

### About Using `io.PipeReader` and `io.PipeWriter`

In the more advanced file test example, `io.Pipe()` is used to connect the decoding process with the input data stream in a concurrent and efficient way. The `PipeWriter` is written to by the goroutine that reads lines from the input file and simulates streaming data as it would arrive over NNTP (including the `\r\n` line endings and the end marker). The `PipeReader` is passed to the decoder, which reads from it as if it were any other `io.Reader`.

This pattern allows you to:
- **Stream data:** Process data as it arrives, without loading the entire file into memory.
- **Simulate network or streaming input:** This is especially useful when testing, or when integrating with NNTP clients/servers.
- **Run decoding and input reading in parallel:** The decoder can process data as soon as it becomes available, increasing throughput.

If you have your data already available as a full in-memory buffer or file, you can pass any `io.Reader` directly to the decoder without needing a pipe.

```go
func TestRapidyencDecoderFiles() (errs []error) {
	files := []string{
		"yenc/multipart_test.yenc",
		"yenc/multipart_test_badcrc.yenc",
		"yenc/singlepart_test.yenc",
		"yenc/singlepart_test_badcrc.yenc",
	}
	for _, fname := range files {
		fmt.Printf("\n=== Testing rapidyenc with file: %s ===\n", fname)
		f, err := os.Open(filepath.Clean(fname))
		if err != nil {
			fmt.Printf("Failed to open %s: %v\n", fname, err)
			continue
		}

		pipeReader, pipeWriter := io.Pipe()
		decoder := AcquireDecoderWithReader(pipeReader)
		decoder.SetDebug(true, true)
		defer ReleaseDecoder(decoder)
		segId := fname
		decoder.SetSegmentId(&segId)

		// Start goroutine to read decoded data
		var decodedData bytes.Buffer
		done := make(chan error, 1)
		go func() {
			buf := make([]byte, DefaultBufSize)
			for {
				n, aerr := decoder.Read(buf)
				if n > 0 {
					decodedData.Write(buf[:n])
				}
				if aerr == io.EOF {
					done <- nil
					return
				}
				if aerr != nil {
					done <- aerr
					return
				}
			}
		}()

		// Write file lines to the pipeWriter
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := scanner.Text()
			if _, err := pipeWriter.Write([]byte(line + "\r\n")); err != nil {
				fmt.Printf("Error writing to pipe: %v\n", err)
				pipeWriter.Close()
				return
			}
		}
		if _, err := pipeWriter.Write([]byte(".\r\n")); err != nil { // NNTP end marker
			fmt.Printf("Error writing end marker to pipe: %v\n", err)
			pipeWriter.Close()
			return
		}
		pipeWriter.Close()
		f.Close()
		if aerr := <-done; aerr != nil {
			err = aerr
			var aBadCrc uint32
			meta := decoder.Meta()
			dlog(always, "DEBUG Decoder error: '%v' (maybe an expected error, check below)\n", err)
			expectedCrc := decoder.ExpectedCrc()
			if expectedCrc != 0 && expectedCrc != meta.Hash {

				// Set aBadCrc based on the file name
				switch fname {
				case "yenc/singlepart_test_badcrc.yenc":
					aBadCrc = 0x6d04a475
				case "yenc/multipart_test_badcrc.yenc":
					aBadCrc = 0xf6acc027
				}
				if aBadCrc > 0 && aBadCrc != meta.Hash {
					fmt.Printf("WARNING1 rapidyenc: CRC mismatch! expected=%#08x | got meta.Hash=%#08x | wanted aBadCrc=%#08x fname: '%s'\n\n", expectedCrc, meta.Hash, aBadCrc, fname)
					errs = append(errs, aerr)
				} else if aBadCrc > 0 && aBadCrc == meta.Hash {
					fmt.Printf("rapidyenc OK expected=%#08x | got meta.Hash=%#08x | wanted aBadCrc=%#08x fname: '%s'\n\n", expectedCrc, meta.Hash, aBadCrc, fname)
				} else if expectedCrc != meta.Hash {
					fmt.Printf("WARNING2 rapidyenc: CRC mismatch! expected=%#08x | got meta.Hash=%#08x | wanted aBadCrc=%#08x fname: '%s'\n\n", expectedCrc, meta.Hash, aBadCrc, fname)
					errs = append(errs, aerr)
				} else {
					fmt.Printf("GOOD CRC matches! aBadCrc=%#08x Name: '%s' fname: '%s'\n", aBadCrc, meta.Name, fname)
				}

			} else if expectedCrc == 0 {
				fmt.Printf("WARNING rapidyenc: No expected CRC set, cannot verify integrity. fname: '%s'\n", fname)
				errs = append(errs, aerr)
			}
		} else {
			meta := decoder.Meta()
			fmt.Printf("OK Decoded %d bytes, CRC32: %#08x, Name: '%s' fname: '%s'\n", decodedData.Len(), meta.Hash, meta.Name, fname)
		}
	}
	return errs
}
```

## API Highlights

- `rapidyenc.AcquireDecoder()`  
  Get a new decoder from the pool (set the reader with `SetReader`).

- `rapidyenc.AcquireDecoderWithReader(io.Reader)`  
  Get a decoder with the reader set.

- `Decoder.Read([]byte)`  
  Stream decoded output, just like an `io.Reader`.

- `Decoder.Meta()`  
  Retrieve decoded file metadata.

- `Decoder.SetDebug(debug1, debug2)`  
  Enable debug logging.

- `rapidyenc.ReleaseDecoder(dec *Decoder)`  
  Return the decoder to the pool for reuse.

## Building from Source

You need a C toolchain and Go >= 1.18. For cross-platform builds, see the [CGO requirements](https://golang.org/cmd/cgo/) and ensure the appropriate static library (`librapidyenc.a`) is built for your target OS/arch.

### Dependencies

- [CGO](https://golang.org/cmd/cgo/)
- [librapidyenc](./src/) (provided C sources)

### Build Matrix Examples

- Linux (amd64/arm64/386/arm)
- Windows (amd64/386/arm)
- macOS (amd64/arm64)

## Contributing

Pull requests and issues are welcome! Please open an issue for bug reports, questions, or feature requests.

## License

[MIT](LICENSE)

---

**Note:** This library is under active development. API changes may occur in the `testing` branch.
