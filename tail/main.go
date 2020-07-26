package main

import (
	"fmt"
	"log"
	"os"

	"zgo.at/follow"
)

func main() {
	if len(os.Args) <= 1 {
		fmt.Println("need at least one filename")
		os.Exit(1)
	}

	f := follow.New()
	go func() { log.Fatal(f.Start(os.Args[1])) }()

	for {
		data := <-f.Data
		if data.Err != nil {
			log.Fatal(data.Err)
		}
		fmt.Println(data)
	}
}
