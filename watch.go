package main

import (
	"context"
	"slices"
	"time"

	"github.com/egoist/mygo/transfer"
)

// clipboardSource is the part of mygo.Clipboard the watcher reads.
type clipboardSource interface {
	Formats() []transfer.Format
	ReadImage() []byte
}

// watcher polls the clipboard and calls onImage with each new image. The
// system tells no one when the clipboard changes, so polling is the only
// way to keep a history.
type watcher struct {
	clip     clipboardSource
	changes  func() int64 // nil where the platform has no change count
	interval time.Duration
	onImage  func([]byte, int64)

	lastChange int64
	lastID     string
}

func (w *watcher) run(ctx context.Context) {
	if w.changes != nil {
		// Images already on the clipboard at launch count as new.
		w.lastChange = -1
	}
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		w.poll()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (w *watcher) poll() {
	if w.changes != nil {
		n := w.changes()
		if n == w.lastChange {
			return
		}
		w.lastChange = n
	}
	formats := w.clip.Formats()
	// Files copied in Finder come with their icons as an image: those are
	// not what the user copied.
	if !slices.Contains(formats, transfer.PNG) || slices.Contains(formats, transfer.FileList) {
		w.lastID = ""
		return
	}
	data := w.clip.ReadImage()
	if len(data) == 0 {
		return
	}
	id := imageID(data)
	// Do not pair bytes from an old clipboard with a newer change count.
	if w.changes != nil && w.changes() != w.lastChange {
		return
	}
	if w.changes != nil || id != w.lastID {
		w.lastID = id
		w.onImage(data, w.lastChange)
	}
}
