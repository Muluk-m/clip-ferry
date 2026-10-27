package main

import (
	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

func appKit() bool {
	_, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	return err == nil && objc.GetClass("NSPasteboard") != 0
}

// clipboardChangeCount returns NSPasteboard's change count, which grows
// whenever any app writes the clipboard: comparing it is free, where
// reading the image to see whether it changed means decoding it.
func clipboardChangeCount() func() int64 {
	if !appKit() {
		return nil
	}
	cls := objc.GetClass("NSPasteboard")
	general, changeCount := objc.RegisterName("generalPasteboard"), objc.RegisterName("changeCount")
	return func() int64 {
		board := objc.ID(cls).Send(general)
		return objc.Send[int64](board, changeCount)
	}
}

// placeTrayRight puts a status item that has no saved place just left of
// the system's icons. macOS adds new items at the left end, the first to
// be hidden when the menu bar runs out of room; it keeps the place the
// user drags the item to under the same key.
func placeTrayRight(autosaveName string, fromRight float64) {
	if !appKit() {
		return
	}
	str := func(s string) objc.ID {
		return objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), s+"\x00")
	}
	defaults := objc.ID(objc.GetClass("NSUserDefaults")).Send(objc.RegisterName("standardUserDefaults"))
	key := str("NSStatusItem Preferred Position " + autosaveName)
	if defaults.Send(objc.RegisterName("objectForKey:"), key) != 0 {
		return
	}
	defaults.Send(objc.RegisterName("setDouble:forKey:"), fromRight, key)
}
