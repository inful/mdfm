//go:build !windows

package mdfm

import "syscall"

func mkfifo(path string) error {
	return syscall.Mkfifo(path, 0o600)
}
