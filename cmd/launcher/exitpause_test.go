package main

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

// failReader fails the test on any read: a launcher that does not own its
// console must not touch stdin at an error exit.
type failReader struct{ t *testing.T }

func (r failReader) Read([]byte) (int, error) {
	r.t.Error("pauseBeforeExit read stdin although the launcher is not alone on its console")
	return 0, io.EOF
}

func TestPauseBeforeExitNotSole(t *testing.T) {
	var out bytes.Buffer
	pauseBeforeExit(false, failReader{t}, &out)
	if out.Len() != 0 {
		t.Errorf("wrote %q, want nothing", out.String())
	}
}

func TestPauseBeforeExitWaitsForOneLine(t *testing.T) {
	in := strings.NewReader("typed\r\nnext line\n")
	var out bytes.Buffer
	pauseBeforeExit(true, in, &out)
	if !strings.Contains(out.String(), exitPauseHint) {
		t.Errorf("output %q lacks the hint %q", out.String(), exitPauseHint)
	}
	if !strings.Contains(exitPauseHint, "Press Enter") {
		t.Errorf("hint %q does not name the key the pause reads up to", exitPauseHint)
	}
	// It reads up to the first line end and not past it.
	rest, _ := io.ReadAll(in)
	if string(rest) != "next line\n" {
		t.Errorf("left %q on stdin, want %q", rest, "next line\n")
	}
}

// errReader returns its bytes, then err.
type errReader struct {
	data []byte
	err  error
}

func (r *errReader) Read(b []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(b, r.data)
	r.data = r.data[n:]
	return n, nil
}

func TestPauseBeforeExitEndsOnReadError(t *testing.T) {
	for _, err := range []error{io.EOF, errors.New("console closed")} {
		done := make(chan struct{})
		go func() {
			pauseBeforeExit(true, &errReader{data: []byte("no line end"), err: err}, io.Discard)
			close(done)
		}()
		<-done // a hang here is a pause that ignores the failed read
	}
}
