package cli

import (
	"fmt"
	"os"
)

func createExport(path string) (*os.File, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("cannot create %s: %w", path, err)
	}
	return f, nil
}
