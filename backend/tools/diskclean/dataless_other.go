//go:build !windows && !darwin

package diskclean

import "io/fs"

func isDataless(fs.FileInfo) bool { return false }
