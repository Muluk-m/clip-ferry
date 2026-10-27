//go:build !darwin

package main

// clipboardChangeCount has no cheap equivalent here: the watcher reads the
// image on each poll and compares its ID instead.
func clipboardChangeCount() func() int64 { return nil }
