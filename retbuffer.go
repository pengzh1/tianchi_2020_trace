package main

import (
	"bytes"
	"io"
	"sync"
)

// 写一个新的traceid+checksum到request body中
func (b *retReader) write(k, v string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	bs := []byte("%2C%22" + k + "%22%3A%22" + v + "%22")
	if bytes.Equal(b.buf[b.writeOff-3:b.writeOff], []byte("%7B")) {
		bs = bs[3:]
	}
	b.buf = append(b.buf, bs...)
	b.writeOff += len(bs)
	return
}
func (b *retReader) setEnd() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.end = true
	b.buf = append(b.buf, []byte("%7D")...)
	b.writeOff += 3
}

type retReader struct {
	mu       sync.Mutex
	buf      []byte // contents are the bytes buf[off : len(buf)]
	off      int    // read at &buf[off], write at &buf[len(buf)]
	writeOff int
	end      bool
}

func newReader() *retReader {
	b := &retReader{
		mu:       sync.Mutex{},
		buf:      make([]byte, 0, 512*1024),
		off:      0,
		writeOff: 10,
		end:      false,
	}
	b.buf = append(b.buf, []byte(`result=%7B`)...)
	return b
}
func (b *retReader) Read(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.end {
		err = io.EOF
	}
	if b.off >= b.writeOff {
		return n, err
	}
	n = copy(p, b.buf[b.off:b.writeOff])
	b.off += n
	return n, err
}
