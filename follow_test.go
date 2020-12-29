package follow

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func start(ctx context.Context, t *testing.T) (Follower, string, chan []string) {
	tmp := filepath.Join(t.TempDir(), "f")
	touch(t, tmp)

	f := New()
	go func() {
		err := f.Start(ctx, tmp)
		if err != nil {
			log.Fatal(err)
		}
	}()
	<-f.Ready

	var ret = make(chan []string)
	go func() {
		var lines []string
		for {
			data := <-f.Data
			if data.Err != nil {
				if data.Err == io.EOF {
					break
				}
				panic(data.Err)
			}
			lines = append(lines, string(data.Bytes))
		}
		ret <- lines
	}()

	return f, tmp, ret
}

func write(t *testing.T, tmp string, lines ...string) []string {
	fp, err := os.OpenFile(tmp, os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		t.Fatal(err)
	}

	for _, l := range lines {
		_, err = fp.WriteString(l + "\n")
		if err != nil {
			t.Fatal(err)
		}
	}
	err = fp.Close()
	if err != nil {
		t.Fatal(err)
	}

	// Give a wee bit of time to actually process this.
	time.Sleep(10 * time.Millisecond)
	return lines
}

func touch(t *testing.T, tmp string) {
	fp, err := os.Create(tmp)
	if err != nil {
		t.Fatal(err)
	}
	fp.Close()
}

func TestFollow(t *testing.T) {
	// Simple case: just write some lines.
	t.Run("simple", func(t *testing.T) {
		f, tmp, lines := start(context.Background(), t)
		want := write(t, tmp, "Hello", "world!")
		f.Stop()
		got := <-lines
		if !reflect.DeepEqual(got, want) {
			t.Errorf("\ngot:  %q\nwant: %q", got, want)
		}
	})

	// Write *a lot* of lines.
	t.Run("many_lines", func(t *testing.T) {
		f, tmp, lines := start(context.Background(), t)
		want := write(t, tmp, repeatSlice(strings.Repeat("x", 1000), 20_000)...)

		// It may take some time for all the lines to be read.
		time.Sleep(1 * time.Second)
		f.Stop()
		got := <-lines
		if !reflect.DeepEqual(got, want) {
			t.Error(len(got), len(want))
		}
	})

	// File gets truncate.
	t.Run("truncate_create", func(t *testing.T) {
		f, tmp, lines := start(context.Background(), t)
		want := write(t, tmp, "before")

		fp, err := os.Create(tmp)
		if err != nil {
			t.Fatal(err)
		}
		fp.Close()

		want = append(want, write(t, tmp, "after")...)
		want = append(want, write(t, tmp, "second")...)

		f.Stop()
		got := <-lines
		if !reflect.DeepEqual(got, want) {
			t.Errorf("\ngot:  %q\nwant: %q", got, want)
		}
	})

	// Truncate with syscall instead of O_TRUNC.
	t.Run("truncate_syscall", func(t *testing.T) {
		f, tmp, lines := start(context.Background(), t)
		want := write(t, tmp, "before")

		err := os.Truncate(tmp, 0)
		if err != nil {
			t.Fatal(err)
		}

		want = append(want, write(t, tmp, "after")...)
		want = append(want, write(t, tmp, "second")...)

		f.Stop()
		got := <-lines
		if !reflect.DeepEqual(got, want) {
			t.Errorf("\ngot:  %q\nwant: %q", got, want)
		}
	})

	// Truncate with syscall instead of O_TRUNC.
	t.Run("truncate_syscall_half", func(t *testing.T) {
		f, tmp, lines := start(context.Background(), t)
		write(t, tmp, "before")

		err := os.Truncate(tmp, 5)
		if err != nil {
			t.Fatal(err)
		}

		write(t, tmp, "after")
		write(t, tmp, "second")

		// This is wrong, but I'm not sure we can do much about this; no real
		// way to detect the truncate offset.
		want := []string{"before", "ter", "second"}

		f.Stop()
		got := <-lines
		if !reflect.DeepEqual(got, want) {
			t.Errorf("\ngot:  %q\nwant: %q", got, want)
		}
	})

	// File gets removed and re-created.
	t.Run("rm", func(t *testing.T) {
		f, tmp, lines := start(context.Background(), t)
		want := write(t, tmp, "before")

		err := os.Remove(tmp)
		if err != nil {
			t.Fatal(err)
		}
		touch(t, tmp)
		want = append(want, write(t, tmp, "after")...)

		f.Stop()
		got := <-lines
		if !reflect.DeepEqual(got, want) {
			t.Errorf("\ngot:  %q\nwant: %q", got, want)
		}
	})

	// Reopen
	t.Run("reopen", func(t *testing.T) {
		f, tmp, lines := start(context.Background(), t)
		want := write(t, tmp, "before")

		f.Reopen <- os.Interrupt

		time.Sleep(10 * time.Millisecond)

		want = append(want, write(t, tmp, "after")...)
		want = append(want, write(t, tmp, "after2")...)

		f.Stop()
		got := <-lines
		if !reflect.DeepEqual(got, want) {
			t.Errorf("\ngot:  %q\nwant: %q", got, want)
		}
	})

	// Context cancellation
	t.Run("cancel", func(t *testing.T) {
		ctx, stop := context.WithCancel(context.Background())
		_, tmp, lines := start(ctx, t)
		want := write(t, tmp, "hello", "world", "xxxx")

		stop()

		got := <-lines
		if !reflect.DeepEqual(got, want) {
			t.Errorf("\ngot:  %q\nwant: %q", got, want)
		}
	})

	// TODO: other edge cases:
	// - Directory disappears/moves?
}

func repeatSlice(s string, n int) (r []string) {
	for i := 0; i < n; i++ {
		r = append(r, s)
	}
	return r
}
