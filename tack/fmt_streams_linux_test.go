//go:build linux

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"unsafe"

	"github.com/tacklang/tack/codegen"
)

// openPTY opens a pseudo-terminal and returns its two ends, the master,
// read by the test, and the slave, a terminal a program's stream can be,
// with output processing off, so the bytes the program writes are read
// back exactly, no newline turned into \r\n. C's stdio sees a terminal
// and buffers stdout by line, as it does for a program run in one. Both
// are closed when the test ends.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	t.Cleanup(func() { master.Close() })
	var unlock int32
	if err := ioctl(master, syscall.TIOCSPTLCK, unsafe.Pointer(&unlock)); err != nil {
		t.Fatalf("unlocking the pseudo-terminal: %v", err)
	}
	var n uint32
	if err := ioctl(master, syscall.TIOCGPTN, unsafe.Pointer(&n)); err != nil {
		t.Fatalf("naming the pseudo-terminal: %v", err)
	}
	slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("opening the pseudo-terminal: %v", err)
	}
	t.Cleanup(func() { slave.Close() })
	var tio syscall.Termios
	if err := ioctl(slave, syscall.TCGETS, unsafe.Pointer(&tio)); err != nil {
		t.Fatalf("reading the terminal's settings: %v", err)
	}
	tio.Oflag &^= syscall.OPOST
	if err := ioctl(slave, syscall.TCSETS, unsafe.Pointer(&tio)); err != nil {
		t.Fatalf("turning output processing off: %v", err)
	}
	return master, slave
}

// ioctl makes the ioctl req on f with the argument at arg.
func ioctl(f *os.File, req uintptr, arg unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, uintptr(arg)); errno != 0 {
		return errno
	}
	return nil
}

// streamTarget is one kind of thing a program's stream can be: a pipe, a
// regular file, or a terminal.
type streamTarget string

const (
	toPipe     streamTarget = "pipe"
	toFile     streamTarget = "file"
	toTerminal streamTarget = "terminal"
)

// attach makes w, the address of one of cmd's streams, be a target of
// the kind k, and returns a function that, once cmd has ended, returns
// every byte cmd wrote to it.
func attach(t *testing.T, cmd *exec.Cmd, w *io.Writer, k streamTarget) func() []byte {
	t.Helper()
	switch k {
	case toPipe:
		var b bytes.Buffer
		*w = &b
		return b.Bytes
	case toFile:
		f, err := os.Create(filepath.Join(t.TempDir(), "stream"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		*w = f
		return func() []byte {
			b, err := os.ReadFile(f.Name())
			if err != nil {
				t.Fatal(err)
			}
			return b
		}
	default:
		master, slave := openPTY(t)
		*w = slave
		// The master is read while the program runs, or a terminal's small
		// buffer fills and the program blocks. Once every copy of the slave
		// is closed, a read returns what is left and then EIO, its end.
		got := make(chan []byte, 1)
		go func() {
			var b bytes.Buffer
			_, err := b.ReadFrom(master)
			if err != nil && !errors.Is(err, syscall.EIO) {
				t.Errorf("reading the terminal: %v", err)
			}
			got <- b.Bytes()
		}()
		// collect closes the slave and waits for the reader, once. It also
		// runs in a cleanup, registered after openPTY's and so run before
		// them, so a test that fails before reading still sees the reader
		// end before the master is closed under it.
		var once sync.Once
		var all []byte
		collect := func() []byte {
			once.Do(func() {
				slave.Close()
				all = <-got
			})
			return all
		}
		t.Cleanup(func() { collect() })
		return collect
	}
}

// fmtStderrWant returns what examples/fmt-stderr writes to standard
// error: its stderr file, then the 1 MiB list twice, byte i of each being
// i % 251.
func fmtStderrWant(t *testing.T) []byte {
	t.Helper()
	want, err := os.ReadFile(filepath.Join(examplesDir, "fmt-stderr", "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		for i := range fmtStderrBig {
			want = append(want, byte(i%251))
		}
	}
	return want
}

// readExampleFile returns the file name of examples/<dir>, failing t when
// it cannot be read.
func readExampleFile(t *testing.T, dir, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(examplesDir, dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestBuiltFmtStreamsWhateverTheTarget builds the examples that write
// through std/fmt to both streams, mixing print, println, printf and
// write on standard output with eprintln, eprintf and ewrite on standard
// error, at every optimization level, and runs each with standard output,
// and then standard error, a pipe, a regular file and a terminal, which
// C's stdio buffers three different ways: each stream must get exactly
// its expected bytes, in call order, and the program its status, however
// the other stream is buffered. examples/printf checks printf's output to
// stdout byte for byte; examples/fmt-exit checks every byte written
// before an exit reaches its stream, past stdio's buffer on stdout and
// through printf's early way out on stderr.
func TestBuiltFmtStreamsWhateverTheTarget(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	examples := []struct {
		name          string
		stdout, stder []byte
		status        int
	}{
		{"printf", readExampleFile(t, "printf", "output"), nil, 0},
		{"fmt-stderr", readExampleFile(t, "fmt-stderr", "output"), fmtStderrWant(t), 0},
		{"fmt-exit", readExampleFile(t, "fmt-exit", "output"), readExampleFile(t, "fmt-exit", "stderr"), 3},
	}
	for _, ex := range examples {
		for _, opt := range codegen.Opts {
			t.Run(ex.name+"/"+string(opt), func(t *testing.T) {
				bin := filepath.Join(t.TempDir(), ex.name)
				r := runTack(t, "-O"+string(opt), "--out", bin, example(ex.name))
				if r.code != exitOK || r.stderr != "" {
					t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
				}
				for _, k := range []streamTarget{toPipe, toFile, toTerminal} {
					for _, which := range []string{"stdout", "stderr"} {
						t.Run(which+" "+string(k), func(t *testing.T) {
							cmd := exec.CommandContext(t.Context(), bin)
							outK, errK := toPipe, toPipe
							if which == "stdout" {
								outK = k
							} else {
								errK = k
							}
							out := attach(t, cmd, &cmd.Stdout, outK)
							errOut := attach(t, cmd, &cmd.Stderr, errK)
							err := cmd.Run()
							code := 0
							var exitErr *exec.ExitError
							if errors.As(err, &exitErr) {
								code = exitErr.ExitCode()
							} else if err != nil {
								t.Fatalf("running %s: %v", bin, err)
							}
							if code != ex.status {
								t.Errorf("exited %d, want %d", code, ex.status)
							}
							checkStream(t, "stdout", out(), ex.stdout)
							checkStream(t, "stderr", errOut(), ex.stder)
						})
					}
				}
			})
		}
	}
}

// checkStream fails t if got, what a program wrote to the stream name, is
// not want, naming the first byte that differs rather than dumping both.
func checkStream(t *testing.T, name string, got, want []byte) {
	t.Helper()
	if bytes.Equal(got, want) {
		return
	}
	i := 0
	for i < len(got) && i < len(want) && got[i] == want[i] {
		i++
	}
	t.Errorf("%s: wrote %d bytes, want %d; first difference at byte %d: got %q, want %q",
		name, len(got), len(want), i, got[i:min(len(got), i+40)], want[i:min(len(want), i+40)])
}

// TestBuiltFmtStreamsInOneTerminalKeepCallOrder does for a terminal what
// TestBuiltFmtStreamsInOnePlaceKeepCallOrder does for a pipe and a file:
// it runs examples/fmt-interleave with both streams one terminal, where
// C's stdio buffers stdout by line, and checks the bytes there are
// exactly examples/fmt-interleave/combined.
func TestBuiltFmtStreamsInOneTerminalKeepCallOrder(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not on PATH")
	}
	want := readExampleFile(t, "fmt-interleave", "combined")
	for _, opt := range codegen.Opts {
		t.Run(string(opt), func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "fmt-interleave")
			r := runTack(t, "-O"+string(opt), "--out", bin, example("fmt-interleave"))
			if r.code != exitOK || r.stderr != "" {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			cmd := exec.CommandContext(t.Context(), bin)
			both := attach(t, cmd, &cmd.Stdout, toTerminal)
			cmd.Stderr = cmd.Stdout
			if err := cmd.Run(); err != nil {
				t.Fatalf("running %s: %v", bin, err)
			}
			checkStream(t, "stdout and stderr", both(), want)
		})
	}
}
