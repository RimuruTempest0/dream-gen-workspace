package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
)

func main() {
	img := image.NewNRGBA(image.Rect(0, 0, 160, 120))
	for y := 0; y < 120; y++ {
		for x := 0; x < 160; x++ {
			img.Set(x, y, color.NRGBA{uint8(210 + x/8), uint8(220 + y/8), 240, 255})
		}
	}
	f, e := os.Create("frontend/tests/fixture.png")
	if e != nil {
		panic(e)
	}
	defer f.Close()
	if e = png.Encode(f, img); e != nil {
		panic(e)
	}
}
