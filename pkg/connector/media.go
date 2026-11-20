package connector

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

// maxDescribeBytes caps the photos describeImage decodes in full for their
// color; bigger ones still get their size, from the header alone.
const maxDescribeBytes = 25 * 1024 * 1024

// describeImage reads what a Peachy bubble shows before a photo downloads:
// its width and height, and its average color as #rrggbb. Anything it can't
// read comes back zero or "", which Peachy reads as "unknown".
func describeImage(data []byte) (width, height int, color string) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, ""
	}
	width, height = cfg.Width, cfg.Height
	if len(data) > maxDescribeBytes {
		return
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return
	}
	return width, height, averageColor(img)
}

// averageColor averages a grid of up to 64×64 samples, which is plenty for
// one placeholder pixel.
func averageColor(img image.Image) string {
	b := img.Bounds()
	if b.Empty() {
		return ""
	}
	stepX, stepY := max(1, b.Dx()/64), max(1, b.Dy()/64)
	var r, g, bl, n uint64
	for y := b.Min.Y; y < b.Max.Y; y += stepY {
		for x := b.Min.X; x < b.Max.X; x += stepX {
			cr, cg, cb, _ := img.At(x, y).RGBA()
			r, g, bl, n = r+uint64(cr>>8), g+uint64(cg>>8), bl+uint64(cb>>8), n+1
		}
	}
	return fmt.Sprintf("#%02x%02x%02x", r/n, g/n, bl/n)
}
