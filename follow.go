package follow

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Data struct {
	Err   error
	Bytes []byte
}

func (d Data) String() string { return string(d.Bytes) }

type Follower struct {
	Data chan Data

	file    string
	split   bufio.SplitFunc
	fp      *os.File
	scanner *bufio.Scanner
	stop    chan error
}

func New() Follower {
	return Follower{
		Data: make(chan Data),
		stop: make(chan error),
	}
}

// Split sets the bufio.Scanner split function.
func (f *Follower) Split(split bufio.SplitFunc) {
	f.split = split
}

// Stop following a file for changes.
func (f Follower) Stop() {
	f.stop <- nil
	f.fp = nil
	f.scanner = nil
}

// Start following a file for changes.
func (f Follower) Start(ctx context.Context, file string) error {
	var err error
	f.file, err = filepath.Abs(file)
	if err != nil {
		return err
	}

	err = f.openFile(false)
	if err != nil {
		return err
	}
	defer f.fp.Close()

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	// Watch the directory rather than the file; there doesn't seem to be any
	// event sent when removing a file (on my Linux system, anyway).
	// TODO: add support for multiple files; we need to be a bit smart about now
	// watching the same dir twice.
	err = w.Add(filepath.Dir(f.file))
	if err != nil {
		return err
	}

	go func() {
		for f.mainloop(ctx, w) {
		}
	}()

	s := <-f.stop
	f.Data <- Data{Err: io.EOF}
	return s
}

func (f *Follower) mainloop(ctx context.Context, w *fsnotify.Watcher) bool {
	select {
	case <-ctx.Done():
		err := ctx.Err()
		if err != nil && err != context.Canceled {
			f.Data <- Data{Err: err}
		}
		f.stop <- nil
		return false
	case err, ok := <-w.Errors:
		if !ok {
			return true
		}
		f.Data <- Data{Err: err}

	case e, ok := <-w.Events:
		if !ok || e.Name != f.file {
			return true
		}

		if e.Op&fsnotify.Write == fsnotify.Write {
			if !f.scanner.Scan() { // io.EOF: file probably got truncated.
				f.scanner = bufio.NewScanner(f.fp)
				if f.split != nil {
					f.scanner.Split(f.split)
				}
				return true
			}

			// We trim the null bytes here as the whole file-truncation
			// thing is messy; sometimes (far from always!) it reads at
			// the wrong offset the the result is filled with null
			// bytes. I tried a bunch of different things to improve on
			// this, but that only made it worse :-/
			// tl;dr: don't truncate files that are in use.
			f.Data <- Data{
				Bytes: bytes.TrimLeft(f.scanner.Bytes(), "\x00"),
				Err:   f.scanner.Err(),
			}

			for f.scanner.Scan() {
				f.Data <- Data{Bytes: f.scanner.Bytes(), Err: f.scanner.Err()}
			}

			// We need to create a new scanner, as it'll always return false
			// after it's "done".
			f.scanner = bufio.NewScanner(f.fp)
			if f.split != nil {
				f.scanner.Split(f.split)
			}
		}

		// File got deleted or moved; attempt to re-open.
		if e.Op&fsnotify.Remove == fsnotify.Remove || e.Op&fsnotify.Rename == fsnotify.Rename {
			f.fp.Close()

			// It may take a bit of time before the new file is created
			// in cases of rename + create.
			var gotit bool
			for i := 0; i < 50; i++ { // Try for 500ms
				err := f.openFile(true)
				if err == nil {
					gotit = true
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !gotit {
				f.Data <- Data{Err: errors.New("follow: file went away and can't reopen")}
			}
		}
	}
	return true
}

func (f *Follower) openFile(reopen bool) error {
	var err error
	f.fp, err = os.Open(f.file)
	if err != nil {
		return err
	}

	if !reopen {
		_, err := f.fp.Seek(0, io.SeekEnd)
		if err != nil {
			return err
		}
	}

	f.scanner = bufio.NewScanner(f.fp)
	if f.split != nil {
		f.scanner.Split(f.split)
	}
	return nil
}
