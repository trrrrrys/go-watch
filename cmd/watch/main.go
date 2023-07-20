package main

import (
	"fmt"
	"os"

	"github.com/trrrrrys/go-watch"
)

func main() {
	if err := watch.Run(); err != nil {
		fmt.Fprintf(os.Stdout, "watch error: %v", err)
		os.Exit(1)
	}
}
