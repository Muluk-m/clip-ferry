package main

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"os"

	"golang.org/x/image/draw"
)

// claudeMaxEdge is the long edge Claude scales larger images down to
// before reading them, so a smaller upload loses nothing it would see.
const claudeMaxEdge = 1568

// upload is the file sent for a shot: its bytes and extension.
type upload struct {
	data []byte
	ext  string
}

// prepareUpload scales an image to maxEdge (0 keeps its size) and keeps
// the smaller of PNG and JPEG, PNG for images with transparency. An image
// already within maxEdge is sent as it is unless JPEG halves it.
func prepareUpload(path string, maxEdge int) (upload, error) {
	orig, err := os.ReadFile(path)
	if err != nil {
		return upload{}, err
	}
	src, err := png.Decode(bytes.NewReader(orig))
	if err != nil {
		return upload{}, err
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	img := src
	scaled := false
	if maxEdge > 0 && max(w, h) > maxEdge {
		if w >= h {
			w, h = maxEdge, max(1, h*maxEdge/w)
		} else {
			w, h = max(1, w*maxEdge/h), maxEdge
		}
		dst := image.NewNRGBA(image.Rect(0, 0, w, h))
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
		img, scaled = dst, true
	}
	if maxEdge == 0 {
		return upload{orig, ".png"}, nil
	}
	best := upload{orig, ".png"}
	if scaled {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return upload{}, err
		}
		best = upload{buf.Bytes(), ".png"}
	}
	if opaque(img) {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
			return upload{}, err
		}
		if buf.Len() < len(best.data)/2 || (scaled && buf.Len() < len(best.data)) {
			best = upload{buf.Bytes(), ".jpg"}
		}
	}
	return best, nil
}

func opaque(img image.Image) bool {
	if o, ok := img.(interface{ Opaque() bool }); ok {
		return o.Opaque()
	}
	return false
}
