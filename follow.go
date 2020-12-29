package follow

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

type Data struct {
	Err   error
	Bytes []byte
}

func (d Data) String() string { return string(d.Bytes) }

type Follower struct {
	Data   chan Data      // Data read from the file.
	Ready  chan struct{}  // Closed if everything is set up.
	Reopen chan os.Signal // Send signal to reopen file.

	// Retry opening the file if it disappears for this period; this will
	// attempt to open the file every second.
	//
	// Default is 2s; set to -1 to retry forever.
	Retry time.Duration

	file string
	fp   *os.File
	fpMu *sync.Mutex
	stop chan error
}

func New() Follower {
	return Follower{
		Ready:  make(chan struct{}),
		Data:   make(chan Data),
		Reopen: make(chan os.Signal, 1),
		Retry:  2 * time.Second,
		stop:   make(chan error),
		fpMu:   new(sync.Mutex),
	}
}

// Stop following a file for changes.
func (f Follower) Stop() {
	f.stop <- nil
	f.fpMu.Lock()
	f.fp = nil
	f.fpMu.Unlock()
}

// Start following a file for changes.
func (f *Follower) Start(ctx context.Context, file string) error {
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

	// Keep reading until we get a stop signal from mainloop.
	go func() {
		for f.mainloop(ctx, w) {
		}
	}()

	close(f.Ready)
	s := <-f.stop
	f.Data <- Data{Err: io.EOF}
	return s
}

// Note: callers should lock!
func (f *Follower) openFile(reopen bool) error {
	fp, err := os.Open(f.file)
	if err != nil {
		return err
	}

	if f.fp != nil {
		*f.fp = *fp
	} else {
		f.fp = fp
	}

	if !reopen {
		_, err := f.fp.Seek(0, io.SeekEnd)
		if err != nil {
			return err
		}
	}

	return nil
}

func (f *Follower) reopen() error {
	f.fpMu.Lock()
	defer f.fpMu.Unlock()

	f.fp.Close()
	err := f.openFile(false)
	if err != nil {
		return err
	}
	return nil
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

	case <-f.Reopen:
		err := f.reopen()
		if err != nil {
			f.Data <- Data{Err: err}
		}

	case e, ok := <-w.Events:
		// Since we read the directory this event may be for another file.
		if !ok || e.Name != f.file {
			return true
		}

		// Write event; read as much data as we can, split it in lines, and send
		// it over the channel.
		if e.Op&fsnotify.Write == fsnotify.Write {
			f.fpMu.Lock()
			d, err := ioutil.ReadAll(f.fp)
			if err != nil {
				f.Data <- Data{Err: err}
			}

			// We didn't read any data, the file may have been truncated. This
			// is not easy to detect since it appears as just a "WRITE" event.
			if len(d) == 0 {
				cur, _ := f.fp.Seek(0, io.SeekCurrent)
				end, _ := f.fp.Seek(0, io.SeekEnd)

				// Seek cursor is past the end of the file, which means it got
				// smaller and (probably) truncated. Seek to the start and read
				// again.
				if cur > end {
					f.fp.Seek(0, io.SeekStart)
					d, err = ioutil.ReadAll(f.fp)
					if err != nil {
						f.Data <- Data{Err: err}
					}
				} else {
					f.fp.Seek(cur, io.SeekStart)
				}
			}

			s := bytes.Split(d, []byte{'\n'})

			// If the last bit of data doesn't end with a newline then seek back
			// so we read it again on the next write event.
			if len(s[len(s)-1]) != 0 {
				seek := len(s[len(s)-1])
				f.fp.Seek(int64(-seek), io.SeekCurrent)
			}
			f.fpMu.Unlock()
			s = s[:len(s)-1]

			for _, ss := range s {
				f.Data <- Data{Bytes: ss}
			}
		}

		// File got deleted or moved; attempt to reopen.
		if e.Op&fsnotify.Remove == fsnotify.Remove || e.Op&fsnotify.Rename == fsnotify.Rename {
			if f.Retry == 0 {
				f.Data <- Data{Err: errors.New("follow: file went away")}
				f.Stop()
				return false
			}

			f.fpMu.Lock()
			defer f.fpMu.Unlock()
			f.fp.Close()

			// Try a few times with a very short sleep; most of the time this is
			// something like Vim writing to the file; we don't need to wait a
			// full second for that.
			for i := 0; i < 10; i++ {
				err := f.openFile(true)
				if err == nil {
					return true
				}
				time.Sleep(25 * time.Millisecond)
			}

			wait := f.Retry
			forever := f.Retry == -1
			for {
				if !forever {
					if wait <= 0 {
						break
					}
					wait -= 1 * time.Second
				}

				time.Sleep(1 * time.Second)

				err := f.openFile(true)
				if err == nil {
					return true
				}
			}
			f.Data <- Data{Err: errors.New("follow: file went away and can't reopen")}
			f.Stop()
			return false
		}
	}
	return true
}
