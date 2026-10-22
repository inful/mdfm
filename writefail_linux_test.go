//go:build linux

package mdfm

import "testing"

// TestWriteFileInPlaceWriteError covers the write and truncate failure paths,
// which no portable test can reach. /dev/full accepts an open and then fails
// every write with ENOSPC, so it is the only fault injection available without
// a special filesystem. It exists on Linux only, hence this file.
func TestWriteFileInPlaceWriteError(t *testing.T) {
	t.Parallel()

	const devFull = "/dev/full"
	if err := writeFileInPlace(devFull, []byte("content\n"), 0o600, nil); err == nil {
		t.Fatalf("expected an error writing to %s", devFull)
	}
}
