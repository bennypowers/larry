package main

import (
	"fmt"
	"os"
)

func main() {
	err := Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "larry: %v\n", err)
	}
	os.Exit(exitCodeFor(err))
}
