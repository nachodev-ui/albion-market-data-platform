package main

import (
	"fmt"
	"log"
	"os"

	"albion-market-data/collector/internal/observability"
)

func main() {
	if isVersionRequest(os.Args[1:]) {
		fmt.Println(formatVersion(observability.CurrentBuildInfo()))
		return
	}

	loadDotEnv()
	storageLock, err := prepareLocalStorage(storagePathsFromArgs(os.Args[1:]))
	if err != nil {
		log.Printf("receiver storage initialization failed: %v", err)
		os.Exit(1)
	}
	defer storageLock.Close()

	if err := run(); err != nil {
		log.Printf("receiver failed: %v", err)
		os.Exit(1)
	}
}
