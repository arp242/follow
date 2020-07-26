Go library to follow a file for changes; e.g. "tail -f".

Typical usage:

```go
f := follow.New()
go func() {
    err := f.Start("file") // Blocks until f.Stop() is called.
    if err != nil {
        log.Fatal(err)
    }
}()

// Read lines forever.
for {
    fmt.Println("OUT", <-f.Data)
}
```
