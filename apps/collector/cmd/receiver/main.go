package main

import (
	"log"
	"os"
)

func main() {
	if err := run(); err != nil {
		log.Printf("receiver failed: %v", err)
		os.Exit(1)
	}
}
