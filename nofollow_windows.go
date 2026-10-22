//go:build windows

package mdfm

// shortcut: Windows has no O_NOFOLLOW, so reads rely on the Lstat check alone
// and keep a narrow time-of-check/time-of-use window. Upgrade to
// FILE_FLAG_OPEN_REPARSE_POINT via x/sys/windows when that window matters.
const noFollow = 0
