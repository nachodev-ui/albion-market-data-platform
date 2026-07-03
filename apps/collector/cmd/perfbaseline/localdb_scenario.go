// Measures the embedded store used by the Albion Online game-data receiver.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"albion-market-data/collector/internal/storage/localdb"
)

func addLocalDatabaseScenario(target *report, root string, samples, size int, batch normalizedOrderBatch) error {
	return addMeasured(target, fmt.Sprintf("update_localdb_%d", size), samples, func() (sampleDetails, error) {
		directory, err := os.MkdirTemp(root, "embedded-")
		if err != nil {
			return sampleDetails{}, err
		}
		defer os.RemoveAll(directory)
		path := filepath.Join(directory, "state.json")
		store, err := localdb.New(path)
		if err != nil {
			return sampleDetails{}, err
		}
		written, duplicates, err := store.AppendOrders(context.Background(), batch)
		return sampleDetails{Artifacts: map[string]int64{"local_database": fileSize(path)}, Counters: map[string]int64{"written": int64(written), "duplicates": int64(duplicates)}}, err
	})
}
