package util

import (
	"io"
	"sort"
)

// SizeReadSeekCloser is an io.Reader, io.Seeker, and io.Closer with a Size
// method.
type SizeReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
	Size() int64
}

// Reader is both an io.Reader and io.ByteReader.
type Reader interface {
	io.Reader
	io.ByteReader
}

// ReadCloser is a Reader that is also an io.Closer.
type ReadCloser interface {
	Reader
	io.Closer
}

type SizeReaderAt interface {
	io.ReaderAt
	Size() int64
}

type nopCloser struct {
	Reader
}

func (nopCloser) Close() error {
	return nil
}

// NopCloser returns a ReadCloser with a no-op Close method wrapping the
// provided Reader r.
func NopCloser(r Reader) ReadCloser {
	return &nopCloser{r}
}

type byteReadCloser struct {
	io.ReadCloser
}

func (rc *byteReadCloser) ReadByte() (byte, error) {
	var b [1]byte

	n, err := rc.Read(b[:])
	if err != nil {
		return 0, err
	}

	if n == 0 {
		return 0, io.ErrNoProgress
	}

	return b[0], nil
}

// ByteReadCloser returns a ReadCloser either by returning the io.ReadCloser
// r if it implements the interface, or wrapping it with a ReadByte method.
func ByteReadCloser(r io.ReadCloser) ReadCloser {
	if rc, ok := r.(ReadCloser); ok {
		return rc
	}

	return &byteReadCloser{r}
}

// NewMultiReaderAt is like io.MultiReader but produces a ReaderAt
// (and Size), instead of just a reader.
func NewMultiReaderAt(parts ...SizeReaderAt) SizeReaderAt {
	m := &multiRA{
		parts: make([]offsetAndSource, 0, len(parts)),
	}
	var off int64
	for _, p := range parts {
		m.parts = append(m.parts, offsetAndSource{off, p})
		off += p.Size()
	}
	m.size = off
	return m
}

type offsetAndSource struct {
	off int64
	SizeReaderAt
}

type multiRA struct {
	parts []offsetAndSource
	size  int64
}

func (m *multiRA) Size() int64 { return m.size }

func (m *multiRA) ReadAt(p []byte, off int64) (n int, err error) {
	wantN := len(p)

	// Skip past the requested offset.
	skipParts := sort.Search(len(m.parts), func(i int) bool {
		// This function returns whether parts[i] will
		// contribute any bytes to our output.
		part := m.parts[i]
		return part.off+part.Size() > off
	})
	parts := m.parts[skipParts:]

	// How far to skip in the first part.
	needSkip := off
	if len(parts) > 0 {
		needSkip -= parts[0].off
	}

	for len(parts) > 0 && len(p) > 0 {
		readP := p
		partSize := parts[0].Size()
		if int64(len(readP)) > partSize-needSkip {
			readP = readP[:partSize-needSkip]
		}
		pn, err0 := parts[0].ReadAt(readP, needSkip)
		if err0 != nil {
			return n, err0
		}
		n += pn
		p = p[pn:]
		if int64(pn)+needSkip == partSize {
			parts = parts[1:]
		}
		needSkip = 0
	}

	if n != wantN {
		err = io.ErrUnexpectedEOF
	}
	return
}

type multiReadCloser struct {
	readClosers []io.ReadCloser
	i           int
}

func (mrc *multiReadCloser) Read(p []byte) (n int, err error) {
	for mrc.i < len(mrc.readClosers) {
		if len(mrc.readClosers) == 1 {
			if rc, ok := mrc.readClosers[0].(*multiReadCloser); ok {
				mrc.readClosers = rc.readClosers

				continue
			}
		}

		n, err = mrc.readClosers[mrc.i].Read(p)
		if err == io.EOF { //nolint:errorlint
			mrc.i++
		}

		if n > 0 || err != io.EOF { //nolint:errorlint
			if err == io.EOF && mrc.i < len(mrc.readClosers) { //nolint:errorlint
				err = nil
			}

			return
		}
	}

	return 0, io.EOF
}

func (mrc *multiReadCloser) Close() (err error) {
	for _, rc := range mrc.readClosers {
		err = rc.Close()
		if err != nil {
			return
		}
	}

	return
}

// MultiReadCloser returns an io.ReadCloser that's the logical concatenation
// of the provider input readers. They're read sequentially. Once all inputs
// have returned io.EOF, Read will return EOF. If any of the readers return
// a non-nil, non-EOF error, Read will return that error.
func MultiReadCloser(readClosers ...io.ReadCloser) io.ReadCloser {
	rc := make([]io.ReadCloser, len(readClosers))
	copy(rc, readClosers)

	return &multiReadCloser{rc, 0}
}

type teeReadCloser struct {
	r io.ReadCloser
	w io.Writer
}

func (t *teeReadCloser) Read(p []byte) (n int, err error) {
	n, err = t.r.Read(p)
	if n > 0 {
		if n, err := t.w.Write(p[:n]); err != nil {
			return n, err
		}
	}

	return
}

func (t *teeReadCloser) Close() error {
	return t.r.Close()
}

// TeeReadCloser returns an io.ReadCloser that writes to w what it reads from
// r. All reads from r performed through it are matched with corresponding
// writes to w. There is no internal buffering - the write must complete
// before the read completes. Any error encountered while writing is reported
// as a read error.
func TeeReadCloser(r io.ReadCloser, w io.Writer) io.ReadCloser {
	return &teeReadCloser{r, w}
}

type teeReaderAt struct {
	r io.ReaderAt
	w io.Writer
}

func (t *teeReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	n, err = t.r.ReadAt(p, off)
	if n > 0 {
		if n, err := t.w.Write(p[:n]); err != nil {
			return n, err
		}
	}

	return
}

// TeeReaderAt returns an io.ReaderAt that writes to w what it reads from r.
// All reads from r performed through it are matched with corresponding writes
// to w.  There is no internal buffering - the write must complete before the
// read completes. Any error encountered while writing is reported as a read
// error.
func TeeReaderAt(r io.ReaderAt, w io.Writer) io.ReaderAt {
	return &teeReaderAt{r, w}
}

type LimitedReadCloser struct {
	R io.ReadCloser
	N int64
}

func (l *LimitedReadCloser) Read(p []byte) (n int, err error) {
	if l.N <= 0 {
		return 0, io.EOF
	}

	if int64(len(p)) > l.N {
		p = p[0:l.N]
	}

	n, err = l.R.Read(p)
	l.N -= int64(n)

	return
}

// Close closes the LimitedReadCloser, rendering it unusable for I/O.
func (l *LimitedReadCloser) Close() error {
	return l.R.Close()
}

// LimitReadCloser returns an io.ReadCloser that reads from r
// but stops with EOF after n bytes.
// The underlying implementation is a *LimitedReadCloser.
func LimitReadCloser(r io.ReadCloser, n int64) io.ReadCloser {
	return &LimitedReadCloser{r, n}
}
