package watchimg

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func testJPEG(t *testing.T, w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 255 / w), uint8(y * 255 / h), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestConvertColor(t *testing.T) {
	bm, err := Convert(testJPEG(t, 800, 600), 192, 192, Color8)
	if err != nil {
		t.Fatal(err)
	}
	if bm.Width != 192 || bm.Height != 144 || len(bm.Data) != 192*144 || bm.RowBytes != 192 {
		t.Fatalf("got %dx%d, %d bytes", bm.Width, bm.Height, len(bm.Data))
	}
	for _, b := range bm.Data {
		if b&0xC0 != 0xC0 {
			t.Fatalf("alpha bits not set: %08b", b)
		}
	}
}

func TestConvertBWNoUpscale(t *testing.T) {
	bm, err := Convert(testJPEG(t, 50, 20), 136, 136, BW1)
	if err != nil {
		t.Fatal(err)
	}
	if bm.Width != 50 || bm.Height != 20 || bm.RowBytes != 7 || len(bm.Data) != 7*20 {
		t.Fatalf("got %+v", bm)
	}
}

func TestBadInput(t *testing.T) {
	if _, err := Convert([]byte("nope"), 100, 100, Color8); err == nil {
		t.Fatal("expected decode error")
	}
	if _, err := Convert(nil, 1000, 100, Color8); err != ErrBadSize {
		t.Fatalf("expected ErrBadSize, got %v", err)
	}
}
