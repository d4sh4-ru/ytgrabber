// Package logging mirrors the standard logger into a size-capped file.
package logging

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

const maxLogSize = 5 << 20 // 5 MiB

// Setup mirrors the standard logger into a file next to the
// database. A packaged app has no visible stdout, so without this every
// log.Printf in the backend would be lost. The previous log is kept as
// ".1" once it grows past maxLogSize.
func Setup(logPath string) (io.Closer, error) {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return nil, err
	}

	if info, err := os.Stat(logPath); err == nil && info.Size() > maxLogSize {
		_ = os.Rename(logPath, logPath+".1")
	}

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}

	log.SetOutput(io.MultiWriter(os.Stderr, file))
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	return file, nil
}
