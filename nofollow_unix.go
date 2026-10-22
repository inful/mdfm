//go:build !windows

package mdfm

import "syscall"

// noFollow fails the open when the final path component is a symlink, closing
// the Lstat-then-read window that lets a symlink be swapped in between the two
// calls.
const noFollow = syscall.O_NOFOLLOW
