//go:build windows

package mdfm

import "errors"

func mkfifo(string) error {
	return errors.New("named pipes are not supported by this test on Windows")
}
