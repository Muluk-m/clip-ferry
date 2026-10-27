package main

import (
	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// clipboardChangeCount returns NSPasteboard's change count, which grows
// whenever any app writes the clipboard: comparing it is free, where
// reading the image to see whether it changed means decoding it.
func clipboardChangeCount() func() int64 {
	if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
		return nil
	}
	cls := objc.GetClass("NSPasteboard")
	if cls == 0 {
		return nil
	}
	general, changeCount := objc.RegisterName("generalPasteboard"), objc.RegisterName("changeCount")
	return func() int64 {
		board := objc.ID(cls).Send(general)
		return objc.Send[int64](board, changeCount)
	}
}
