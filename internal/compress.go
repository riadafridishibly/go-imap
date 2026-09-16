package internal

import (
	"compress/flate"
)

// FlateWriter is a flate.Writer which flushes after every write.
//
// Without the flush, the compressor keeps data in its internal buffer and the
// other side never receives the command or response.
type FlateWriter struct {
	*flate.Writer
}

func (w FlateWriter) Write(b []byte) (int, error) {
	n, err := w.Writer.Write(b)
	if err != nil {
		return n, err
	}
	return n, w.Writer.Flush()
}
