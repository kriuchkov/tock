//go:build !unix

package fileio

import "os"

// Without flock the update runs unlocked; writes stay atomic.
func lock(*os.File) error { return nil }

func tryLock(*os.File) (bool, error) { return true, nil }

func unlock(*os.File) error { return nil }
