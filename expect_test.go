package rapidyenc

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// part encodes raw as part partNumber of totalParts of a fileSize byte file
// starting at offset, followed by the NNTP terminator.
func part(raw []byte, name string, fileSize, offset int64, partNumber, totalParts int64) ([]byte, error) {
	w := new(bytes.Buffer)

	enc, err := NewEncoder(w, Meta{
		FileName:   name,
		FileSize:   fileSize,
		PartSize:   int64(len(raw)),
		Offset:     offset,
		PartNumber: partNumber,
		TotalParts: totalParts,
	})
	if err != nil {
		return nil, err
	}
	if _, err := enc.Write(raw); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	if _, err := w.Write([]byte(".\r\n")); err != nil {
		return nil, err
	}

	return w.Bytes(), nil
}

func TestExpectPairsRequestsInOrder(t *testing.T) {
	sizes := []int{1000, 50_000, 300, 120_000}

	stream := new(bytes.Buffer)
	raws := make([][]byte, len(sizes))
	for i, size := range sizes {
		raws[i] = bytes.Repeat([]byte("abcdefgh"), size/8+1)[:size]
		encoded, err := body(raws[i])
		require.NoError(t, err)
		_, err = io.Copy(stream, encoded)
		require.NoError(t, err)
	}

	dec := NewDecoder(stream, WithStatusLineAlreadyRead())
	for i := range sizes {
		require.NoError(t, dec.Expect(fmt.Sprintf("request-%d", i), nil))
	}
	require.Equal(t, len(sizes), dec.Expected())
	require.Equal(t, []any{"request-0", "request-1", "request-2", "request-3"}, dec.Pending())

	for i, size := range sizes {
		response, err := dec.Next()
		require.NoError(t, err)
		require.Equal(t, fmt.Sprintf("request-%d", i), response.Request)
		require.Equal(t, raws[i], response.Data, "size=%d", size)
		require.Equal(t, len(sizes)-i-1, dec.Expected())
	}
}

// The sink is bound to the request, so the part order on the wire does not have to
// match the part order in the file - Meta.Offset places each one.
func TestExpectWriterAtOutOfOrder(t *testing.T) {
	const partSize = 10_000
	const parts = 5

	raw := make([]byte, partSize*parts)
	_, err := rand.New(rand.NewSource(7)).Read(raw)
	require.NoError(t, err)

	encoded := make([][]byte, parts)
	for i := range parts {
		offset := int64(i * partSize)
		encoded[i], err = part(raw[offset:offset+partSize], "out-of-order.bin",
			int64(len(raw)), offset, int64(i+1), parts)
		require.NoError(t, err)
	}

	f, err := os.Create(filepath.Join(t.TempDir(), "out-of-order.bin"))
	require.NoError(t, err)
	defer f.Close()

	order := []int{3, 0, 4, 1, 2}

	stream := new(bytes.Buffer)
	dec := NewDecoder(stream, WithStatusLineAlreadyRead())
	for _, i := range order {
		stream.Write(encoded[i])
		require.NoError(t, dec.Expect(i, f))
	}

	for _, i := range order {
		response, err := dec.Next()
		require.NoError(t, err, "part=%d", i+1)
		require.Equal(t, i, response.Request)
		require.Nil(t, response.Data)
		require.False(t, response.SinkFailed)
		require.Equal(t, int64(i*partSize), response.Metadata.Offset)
	}

	assembled, err := os.ReadFile(f.Name())
	require.NoError(t, err)
	require.Equal(t, raw, assembled)
}

func TestExpectWriterSequential(t *testing.T) {
	raw := bytes.Repeat([]byte("abcdefgh"), 20_000)

	encoded, err := body(raw)
	require.NoError(t, err)

	out := new(bytes.Buffer)
	dec := NewDecoder(encoded, WithStatusLineAlreadyRead())
	require.NoError(t, dec.Expect("request", out))

	response, err := dec.Next()
	require.NoError(t, err)
	require.Equal(t, raw, out.Bytes())
	require.Nil(t, response.Data)
	require.Equal(t, int64(len(raw)), response.Metadata.BytesProduced)
}

func TestExpectNilSinkBuffers(t *testing.T) {
	raw := bytes.Repeat([]byte("abcdefgh"), 1000)

	encoded, err := body(raw)
	require.NoError(t, err)

	dec := NewDecoder(encoded, WithStatusLineAlreadyRead())
	require.NoError(t, dec.Expect("request", nil))

	response, err := dec.Next()
	require.NoError(t, err)
	require.Equal(t, "request", response.Request)
	require.Equal(t, raw, response.Data)
}

// uu carries no offset for a part to be placed at, so it must never reach the sink.
func TestExpectSkipsUU(t *testing.T) {
	const dataLine = "M86%A86%A86%A86%A86%A86%A86%A86%A86%A86%A86%A86%A86%A86%A86%A"
	raw := "222 Body follows\r\nbegin 644 test.bin\r\n" + dataLine + "\r\n`\r\nend\r\n.\r\n"

	sink := new(countingWriterAt)
	dec := NewDecoder(strings.NewReader(raw))
	require.NoError(t, dec.Expect("request", sink))

	response, err := dec.Next()
	require.NoError(t, err)
	require.Zero(t, sink.writes, "uu must not be routed to the sink")
	require.Equal(t, FormatUU, response.Metadata.Format)
	require.Equal(t, bytes.Repeat([]byte("a"), 45), response.Data)
}

type countingWriterAt struct{ writes int }

func (c *countingWriterAt) WriteAt(p []byte, off int64) (int, error) {
	c.writes++
	return len(p), nil
}

var errSinkClosed = errors.New("sink closed")

type failingWriterAt struct{}

func (failingWriterAt) WriteAt(p []byte, off int64) (int, error) { return 0, errSinkClosed }

// A failed write must not abandon the decoder mid-response. The rest of the article
// would be read as the start of the next one, costing the whole connection.
func TestExpectSinkFailureDrainsResponse(t *testing.T) {
	first := bytes.Repeat([]byte("abcdefgh"), 20_000)
	second := bytes.Repeat([]byte("12345678"), 20_000)

	stream := new(bytes.Buffer)
	for _, raw := range [][]byte{first, second} {
		encoded, err := body(raw)
		require.NoError(t, err)
		_, err = io.Copy(stream, encoded)
		require.NoError(t, err)
	}

	dec := NewDecoder(stream, WithStatusLineAlreadyRead())
	require.NoError(t, dec.Expect("fails", failingWriterAt{}))
	require.NoError(t, dec.Expect("succeeds", nil))

	failed, err := dec.Next()
	require.NoError(t, err, "the failure belongs on the response, not to Next")
	require.Equal(t, "fails", failed.Request)
	require.True(t, failed.SinkFailed)
	require.ErrorIs(t, failed.SinkError, errSinkClosed)
	require.Nil(t, failed.Data, "the body must be discarded, not buffered instead")
	require.Equal(t, int64(len(first)), failed.Metadata.BytesProduced,
		"the response must still be read to its end")

	// The connection is still usable, which is the whole point
	ok, err := dec.Next()
	require.NoError(t, err)
	require.Equal(t, "succeeds", ok.Request)
	require.False(t, ok.SinkFailed)
	require.Equal(t, second, ok.Data)
}

func TestExpectInvalidSink(t *testing.T) {
	dec := NewDecoder(strings.NewReader(""))
	require.Error(t, dec.Expect("request", "not a writer"))
	require.Zero(t, dec.Expected())
}

func TestClearExpected(t *testing.T) {
	dec := NewDecoder(strings.NewReader(""))
	require.NoError(t, dec.Expect("a", nil))
	require.NoError(t, dec.Expect("b", nil))
	require.Equal(t, []any{"a", "b"}, dec.Pending())

	dec.ClearExpected()
	require.Zero(t, dec.Expected())
	require.Empty(t, dec.Pending())
}

// Without Expect nothing changes, so existing callers are unaffected.
func TestWithoutExpect(t *testing.T) {
	raw := bytes.Repeat([]byte("abcdefgh"), 1000)

	encoded, err := body(raw)
	require.NoError(t, err)

	dec := NewDecoder(encoded, WithStatusLineAlreadyRead())
	response, err := dec.Next()
	require.NoError(t, err)
	require.Nil(t, response.Request)
	require.Equal(t, raw, response.Data)
}

// A CRC mismatch is only detectable at "=yend", after the part has been written.
func TestSinkReportsCrcMismatchAfterWriting(t *testing.T) {
	raw := bytes.Repeat([]byte("abcdefgh"), 1000)

	encoded, err := body(raw)
	require.NoError(t, err)

	all, err := io.ReadAll(encoded)
	require.NoError(t, err)
	i := bytes.LastIndex(all, []byte("crc32="))
	require.NotEqual(t, -1, i)
	copy(all[i+len("crc32="):], []byte("deadbeef"))

	out := new(bytes.Buffer)
	dec := NewDecoder(bytes.NewReader(all), WithStatusLineAlreadyRead())
	require.NoError(t, dec.Expect("request", out))

	_, err = dec.Next()
	require.ErrorIs(t, err, ErrCrcMismatch)
	require.Equal(t, raw, out.Bytes(), "the payload was written before it could be validated")
}

// A sink must not allocate in proportion to the payload when the headers do not
// declare a size, as XZVER and XZHDR responses do not.
func TestSinkBoundedAllocationForUnknownSize(t *testing.T) {
	const payload = 8 << 20

	encoded := unknownSizeArticle(payload)

	buffered := allocatedBytes(t, func() {
		dec := NewDecoder(bytes.NewReader(encoded), WithStatusLineAlreadyRead())
		response, err := dec.Next()
		require.NoError(t, err)
		require.Equal(t, payload, len(response.Data))
	})

	// The destination is the caller's, so it is allocated outside what is measured
	w := bytes.NewBuffer(make([]byte, 0, payload))
	written := allocatedBytes(t, func() {
		w.Reset()
		dec := NewDecoder(bytes.NewReader(encoded), WithStatusLineAlreadyRead())
		require.NoError(t, dec.Expect(nil, w))
		_, err := dec.Next()
		require.NoError(t, err)
		require.Equal(t, payload, w.Len())
	})

	require.Greater(t, buffered, int64(2*payload),
		"buffering an unknown size is expected to cost several times the payload")
	require.Less(t, written, int64(payload/8),
		"a sink must not allocate in proportion to the payload, got %d for %d bytes", written, payload)
}

func unknownSizeArticle(payload int) []byte {
	var b bytes.Buffer
	b.WriteString("=ybegin line=128 size=-1 name=unknown.bin\r\n")

	raw := bytes.Repeat([]byte("abcdefgh"), payload/8)

	// Encoded by hand so the =ybegin above is the only header. The payload has no
	// character needing an escape, so every byte is one encoded byte.
	line := make([]byte, 0, 130)
	for i, c := range raw {
		line = append(line, c+42)
		if (i+1)%128 == 0 {
			line = append(line, '\r', '\n')
			b.Write(line)
			line = line[:0]
		}
	}
	if len(line) > 0 {
		line = append(line, '\r', '\n')
		b.Write(line)
	}
	b.WriteString(fmt.Sprintf("=yend size=%d\r\n.\r\n", len(raw)))

	return b.Bytes()
}

func allocatedBytes(t *testing.T, fn func()) int64 {
	t.Helper()
	return testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			fn()
		}
	}).AllocedBytesPerOp()
}

func BenchmarkUnknownSize(b *testing.B) {
	const payload = 8 << 20

	encoded := bytes.NewReader(unknownSizeArticle(payload))

	b.Run("Data", func(b *testing.B) {
		_, err := encoded.Seek(0, io.SeekStart)
		require.NoError(b, err)

		b.SetBytes(payload)
		dec := NewDecoder(encoded, WithStatusLineAlreadyRead())
		b.ResetTimer()
		for b.Loop() {
			response, err := dec.Next()
			require.NoError(b, err)
			require.Equal(b, payload, len(response.Data))

			_, err = encoded.Seek(0, io.SeekStart)
			require.NoError(b, err)
		}
	})

	b.Run("Sink", func(b *testing.B) {
		_, err := encoded.Seek(0, io.SeekStart)
		require.NoError(b, err)

		b.SetBytes(payload)
		w := bytes.NewBuffer(make([]byte, 0, payload))
		dec := NewDecoder(encoded, WithStatusLineAlreadyRead())
		b.ResetTimer()
		for b.Loop() {
			w.Reset()
			require.NoError(b, dec.Expect(nil, w))
			_, err := dec.Next()
			require.NoError(b, err)
			require.Equal(b, payload, w.Len())

			_, err = encoded.Seek(0, io.SeekStart)
			require.NoError(b, err)
		}
	})
}

// gatedReader hands out data up to a point, then blocks until released, so a test can
// observe the decoder while a response is part way through.
type gatedReader struct {
	data     []byte
	pos      int
	gate     int
	reached  chan struct{}
	release  chan struct{}
	signaled bool
}

func (g *gatedReader) Read(p []byte) (int, error) {
	if g.pos >= g.gate && !g.signaled {
		g.signaled = true
		close(g.reached)
		<-g.release
	}
	if g.pos >= len(g.data) {
		return 0, io.EOF
	}
	n := copy(p, g.data[g.pos:min(g.pos+g.gate, len(g.data))])
	g.pos += n

	return n, nil
}

// A request is paired as soon as its response starts arriving, so counting only
// unpaired requests would report a connection mid-article as idle.
func TestExpectedCountsResponseBeingDecoded(t *testing.T) {
	raw := bytes.Repeat([]byte("abcdefgh"), 20_000)

	encoded, err := body(raw)
	require.NoError(t, err)
	all, err := io.ReadAll(encoded)
	require.NoError(t, err)

	r := &gatedReader{
		data:    all,
		gate:    4096,
		reached: make(chan struct{}),
		release: make(chan struct{}),
	}

	dec := NewDecoder(r, WithStatusLineAlreadyRead())
	require.NoError(t, dec.Expect("in-flight", nil))
	require.Equal(t, 1, dec.Expected())

	done := make(chan *Response, 1)
	go func() {
		response, err := dec.Next()
		require.NoError(t, err)
		done <- response
	}()

	<-r.reached
	require.Equal(t, 1, dec.Expected(), "the response being decoded still counts")
	require.Equal(t, []any{"in-flight"}, dec.Pending())

	close(r.release)
	response := <-done
	require.Equal(t, "in-flight", response.Request)
	require.Equal(t, raw, response.Data)
	require.Zero(t, dec.Expected())
	require.Empty(t, dec.Pending())
}

// The queue is the only state a second goroutine touches, so Expect, Pending and
// ClearExpected must be safe against a decoding goroutine. Run with -race.
func TestQueueConcurrentWithNext(t *testing.T) {
	const responses = 50

	raw := bytes.Repeat([]byte("abcdefgh"), 500)

	stream := new(bytes.Buffer)
	for range responses {
		encoded, err := body(raw)
		require.NoError(t, err)
		_, err = io.Copy(stream, encoded)
		require.NoError(t, err)
	}

	dec := NewDecoder(stream, WithStatusLineAlreadyRead())

	sent := make(chan struct{})
	go func() {
		defer close(sent)
		for i := range responses {
			require.NoError(t, dec.Expect(i, nil))
		}
	}()

	watching := make(chan struct{})
	stop := make(chan struct{})
	go func() {
		defer close(watching)
		for {
			select {
			case <-stop:
				return
			default:
				dec.Pending()
				dec.Expected()
			}
		}
	}()

	for range responses {
		response, err := dec.Next()
		require.NoError(t, err)
		require.Equal(t, raw, response.Data)
	}

	<-sent
	close(stop)
	<-watching
}

// The download case: the final name is not known until a part has been decoded, so
// the caller writes every part into one file it named itself and renames later. Parts
// arrive on separate connections, so separate Decoders share the file. Run with -race.
func TestExpectConcurrentDecodersOneFile(t *testing.T) {
	const partSize = 20_000
	const parts = 8

	raw := make([]byte, partSize*parts)
	_, err := rand.New(rand.NewSource(11)).Read(raw)
	require.NoError(t, err)

	encoded := make([][]byte, parts)
	for i := range parts {
		offset := int64(i * partSize)
		encoded[i], err = part(raw[offset:offset+partSize], "final-name.bin",
			int64(len(raw)), offset, int64(i+1), parts)
		require.NoError(t, err)
	}

	temporary := filepath.Join(t.TempDir(), "incomplete.tmp")
	f, err := os.Create(temporary)
	require.NoError(t, err)
	defer f.Close()

	names := make([]string, parts)
	done := make(chan struct{})
	for i := range parts {
		go func() {
			defer func() { done <- struct{}{} }()

			dec := NewDecoder(bytes.NewReader(encoded[i]), WithStatusLineAlreadyRead())
			require.NoError(t, dec.Expect(i, f))

			response, err := dec.Next()
			require.NoError(t, err)
			require.False(t, response.SinkFailed)
			require.Nil(t, response.Data)
			names[i] = response.Metadata.FileName
		}()
	}
	for range parts {
		<-done
	}

	assembled, err := os.ReadFile(temporary)
	require.NoError(t, err)
	require.Equal(t, raw, assembled)

	// The name is only known once a part has been decoded, and what to do with it is
	// the caller's decision
	for i, name := range names {
		require.Equal(t, "final-name.bin", name, "part=%d", i+1)
	}
	require.NoError(t, f.Close())
	require.NoError(t, os.Rename(temporary, filepath.Join(filepath.Dir(temporary), names[0])))
}
