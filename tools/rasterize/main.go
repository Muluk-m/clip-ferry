// Rasterize draws an SVG into a PNG of the given size, with MyGo's own
// renderer, which keeps the transparency qlmanage fills in:
//
//	go run ./tools/rasterize resources/logo.svg resources/icon.png 1024
package main

import (
	"image/png"
	"log"
	"os"
	"strconv"

	"github.com/egoist/mygo/ui"
)

func main() {
	if len(os.Args) != 4 {
		log.Fatal("usage: rasterize in.svg out.png size")
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	size, err := strconv.Atoi(os.Args[3])
	if err != nil {
		log.Fatal(err)
	}
	svg := ui.MustParseSVG(data)
	t := ui.NewTester(func(c *ui.Context) {
		c.Root().Background(ui.Transparent)
		ui.Image(c, svg).Size(float32(size), float32(size))
	}, size, size)
	t.SetScale(1)
	out, err := os.Create(os.Args[2])
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()
	if err := png.Encode(out, t.Image()); err != nil {
		log.Fatal(err)
	}
}
