package gui

import (
	"image"
	"image/png"
	"io"
)

// writePNG encodes a top-down RGBA buffer as a PNG.
func writePNG(destination io.Writer, rgba []byte, width, height int) error {
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	copy(canvas.Pix, rgba)
	return png.Encode(destination, canvas)
}
