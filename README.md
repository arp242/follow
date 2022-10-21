Go library to follow a file for changes; e.g. "tail -F".

There's a little example application in [`tail/main.go`](/tail/main.go):

```go
func main() {
	if len(os.Args) <= 1 {
		fmt.Println("need at least one filename")
		os.Exit(1)
	}

	f := follow.New()

	// Maximum time to retry opening the file after it goes away; -1 to keep
	// trying forever.
	f.Retry = -1

	// Install signal handler; any signal sent to this will reopen the file; you
	// can send something manually with:
	//    f.Reopen <- os.Interrupt
	signal.Notify(f.Reopen, syscall.SIGHUP)

	// Keep reading data in the background, sending it to the f.Data channel.
	go func() { log.Fatal(f.Start(context.Background(), os.Args[1])) }()

	for {
		data := <-f.Data
		if data.Err != nil {
			if data.Err == io.EOF {
				break
			}
			log.Fatal(data.Err)
		}
		fmt.Println("X", data)
	}
}
```
