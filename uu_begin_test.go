package rapidyenc

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// A "begin <mode> <name>" header identifies uu and carries the filename. The
// format is also recognised from the data lines alone, so a broken header only
// costs the filename - which is what made this easy to miss.
func TestUUBeginHeader(t *testing.T) {
	const dataLine = "M86%A86%A86%A86%A86%A86%A86%A86%A86%A86%A86%A86%A86%A86%A86%A"

	cases := []struct {
		header   string
		fileName string
	}{
		{"begin 644 test.bin", "test.bin"},
		{"begin 755 with space.bin", "with space.bin"},
		{"begin  644  extra spaces.bin", "extra spaces.bin"},
		{"begin 644", ""},            // no filename
		{"begin xyz bad.bin", ""},    // mode is not octal
		{"begin 899 bad.bin", ""},    // digits outside 0-7
		{"begin  ", ""},              // nothing after begin
		{"beginning of a story", ""}, // not a header at all
	}

	for _, tc := range cases {
		t.Run(tc.header, func(t *testing.T) {
			body := "222 Body follows\r\n" + tc.header + "\r\n" + dataLine + "\r\n`\r\nend\r\n.\r\n"
			dec := NewDecoder(strings.NewReader(body))
			response, err := dec.Next()
			require.NoError(t, err)
			require.Equal(t, FormatUU, response.Metadata.Format)
			require.Equal(t, tc.fileName, response.Metadata.FileName)
			require.Equal(t, bytes.Repeat([]byte("a"), 45), response.Data)
		})
	}
}

func TestUUBeginHeaderFromFile(t *testing.T) {
	f, err := os.Open("testdata/logo_full.uu")
	require.NoError(t, err)
	defer f.Close()

	w := new(bytes.Buffer)
	_, err = io.Copy(w, f)
	require.NoError(t, err)
	w.WriteString(".\r\n")

	dec := NewDecoder(w, WithStatusLineAlreadyRead())
	response, err := dec.Next()
	require.NoError(t, err)
	require.Equal(t, FormatUU, response.Metadata.Format)
	require.NotEmpty(t, response.Metadata.FileName)
}
