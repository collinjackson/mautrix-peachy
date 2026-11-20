package connector

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestDescribeImage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 128, B: 0, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	w, h, c := describeImage(buf.Bytes())
	if w != 4 || h != 3 || c != "#ff8000" {
		t.Errorf("got %d×%d %q", w, h, c)
	}
	if w, h, c := describeImage([]byte("not an image")); w != 0 || h != 0 || c != "" {
		t.Errorf("non-image: %d×%d %q", w, h, c)
	}
}
