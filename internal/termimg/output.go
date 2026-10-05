package termimg

import (
	"os"
	"sync"
)

// Output is the terminal, shared by the screen renderer and the image
// writer: each write goes out whole, so an image sent from a background
// goroutine never lands in the middle of a frame (which garbles both).
type Output struct {
	*os.File
	mu sync.Mutex
}

// NewOutput wraps f.
func NewOutput(f *os.File) *Output { return &Output{File: f} }

func (o *Output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.File.Write(p)
}

func (o *Output) WriteString(s string) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.File.WriteString(s)
}
