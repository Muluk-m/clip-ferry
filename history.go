package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/image/draw"
)

// thumbWidth is the width in pixels of the thumbnails kept beside each
// image: the list shows them small, so decoding full screenshots for it
// would only cost memory.
const thumbWidth = 240

// shot is one image taken from the clipboard, kept on disk as
// images/<ID>.png with a thumbnail in thumbs/<ID>.png.
type shot struct {
	ID    string
	Path  string
	Thumb string
	Taken time.Time
	W, H  int
}

// history keeps the images in a directory. Its methods do file I/O and
// run on worker goroutines; the view sees shots through Window.Update.
type history struct {
	dir string
	max int
}

func (h *history) imagesDir() string { return filepath.Join(h.dir, "images") }
func (h *history) thumbsDir() string { return filepath.Join(h.dir, "thumbs") }

func (h *history) shot(id string, taken time.Time, w, hgt int) shot {
	return shot{
		ID:    id,
		Path:  filepath.Join(h.imagesDir(), id+".png"),
		Thumb: filepath.Join(h.thumbsDir(), id+".png"),
		Taken: taken,
		W:     w,
		H:     hgt,
	}
}

// imageID names an image by its content, so copying the same image again
// brings the existing one to the top rather than storing a copy.
func imageID(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

// add stores PNG data, newest first, and drops the oldest images past max.
// It reports whether the image was new.
func (h *history) add(data []byte, now time.Time) (shot, bool, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return shot{}, false, err
	}
	s := h.shot(imageID(data), now, cfg.Width, cfg.Height)
	if _, err := os.Stat(s.Path); err == nil {
		// Seen before: touch it so it sorts as the newest.
		return s, false, os.Chtimes(s.Path, now, now)
	}
	if err := os.MkdirAll(h.imagesDir(), 0o700); err != nil {
		return shot{}, false, err
	}
	if err := os.MkdirAll(h.thumbsDir(), 0o700); err != nil {
		return shot{}, false, err
	}
	if err := writeThumb(s.Thumb, data); err != nil {
		return shot{}, false, err
	}
	if err := os.WriteFile(s.Path, data, 0o600); err != nil {
		return shot{}, false, err
	}
	if err := os.Chtimes(s.Path, now, now); err != nil {
		return shot{}, false, err
	}
	return s, true, h.trim()
}

// list returns the stored images, newest first.
func (h *history) list() ([]shot, error) {
	entries, err := os.ReadDir(h.imagesDir())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var shots []shot
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".png")
		if !ok || e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		s := h.shot(id, info.ModTime(), 0, 0)
		if f, err := os.Open(s.Path); err == nil {
			if cfg, err := png.DecodeConfig(f); err == nil {
				s.W, s.H = cfg.Width, cfg.Height
			}
			f.Close()
		}
		shots = append(shots, s)
	}
	sort.Slice(shots, func(i, j int) bool { return shots[i].Taken.After(shots[j].Taken) })
	return shots, nil
}

func (h *history) trim() error {
	shots, err := h.list()
	if err != nil || len(shots) <= h.max {
		return err
	}
	for _, s := range shots[h.max:] {
		h.remove(s)
	}
	return nil
}

func (h *history) remove(s shot) {
	os.Remove(s.Path)
	os.Remove(s.Thumb)
}

func (h *history) clear() error {
	if err := os.RemoveAll(h.imagesDir()); err != nil {
		return err
	}
	return os.RemoveAll(h.thumbsDir())
}

func writeThumb(path string, data []byte) error {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	b := src.Bounds()
	w, hgt := b.Dx(), b.Dy()
	if w > thumbWidth {
		w, hgt = thumbWidth, max(1, hgt*thumbWidth/w)
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, hgt))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o600)
}
