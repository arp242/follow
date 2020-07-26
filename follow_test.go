package follow_test

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"zgo.at/follow"
)

func TestFollow(t *testing.T) {
	t.Skip() // TODO: write some actual tests.

	f := follow.New()

	go func() { t.Fatal(f.Start("f")) }()

	go func() {
		for {
			fmt.Println("OUT", <-f.Data)
		}
	}()

	fp, err := os.Create("f")
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i <= 20; i++ {
		_, err = fp.WriteString(strconv.Itoa(i) + "\n")
		if err != nil {
			t.Fatal(err)
		}
		fp.WriteString("CZCCZXCXZ\n")
		time.Sleep(4 * time.Millisecond)
		fp.Truncate(0)
	}
	err = fp.Close()
	if err != nil {
		t.Fatal(err)
	}
}

/*
func TestFollow(t *testing.T) {
	ch := make(chan follow.Data)
	go func() {
		t.Fatal(follow.Follow("f", ch, nil))
	}()
	go func() {
		for {
			d := <-ch
			fmt.Printf("OUT %s %q\n", d.Err, d.Bytes)
		}
	}()

	fp, err := os.Create("f")
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i <= 20; i++ {
		_, err = fp.WriteString(strconv.Itoa(i) + "\n")
		if err != nil {
			t.Fatal(err)
		}
		fp.WriteString("CZCCZXCXZ\n")
		time.Sleep(4 * time.Millisecond)
		fp.Truncate(0)
	}
	err = fp.Close()
	if err != nil {
		t.Fatal(err)
	}
}
*/
