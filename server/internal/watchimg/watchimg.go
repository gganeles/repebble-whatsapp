// Package watchimg converts photos into raw Pebble bitmaps: scaled to fit the
// watch, Floyd–Steinberg dithered to the 64-colour Pebble palette (or black and
// white), and packed in the layout gbitmap expects, minus row padding.
package watchimg

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

type Format string

const (
	// Color8 is one byte per pixel, GColor8 layout 0bAARRGGBB with alpha 0b11.
	Color8 Format = "color"
	// BW1 is one bit per pixel, least significant bit is the leftmost pixel,
	// each row padded to a whole byte. 1 = white.
	BW1 Format = "bw"
)

// Max dimensions accepted from clients; the Time 2 screen is 200x228.
const (
	MaxWidth  = 200
	MaxHeight = 228
)

type Bitmap struct {
	Width, Height int
	Format        Format
	// RowBytes is the packed row length in Data.
	RowBytes int
	Data     []byte
}

var ErrBadSize = errors.New("image size out of range")

// Convert decodes src (JPEG, PNG, GIF or WebP) and fits it inside maxW x maxH.
func Convert(src []byte, maxW, maxH int, format Format) (*Bitmap, error) {
	if maxW < 8 || maxH < 8 || maxW > MaxWidth || maxH > MaxHeight {
		return nil, ErrBadSize
	}
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	return FromImage(img, maxW, maxH, format), nil
}

// FromImage scales and dithers an already decoded image.
func FromImage(img image.Image, maxW, maxH int, format Format) *Bitmap {
	w, h := fit(img.Bounds().Dx(), img.Bounds().Dy(), maxW, maxH)
	scaled := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(scaled, scaled.Bounds(), img, img.Bounds(), draw.Src, nil)
	if format == BW1 {
		return ditherBW(scaled)
	}
	return ditherColor(scaled)
}

func fit(w, h, maxW, maxH int) (int, int) {
	if w <= 0 || h <= 0 {
		return 1, 1
	}
	// Never upscale; keep the aspect ratio.
	scale := min(float64(maxW)/float64(w), float64(maxH)/float64(h), 1)
	return max(1, int(float64(w)*scale+0.5)), max(1, int(float64(h)*scale+0.5))
}

func clamp(v int32) int32 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

// errBuf holds per-channel error for the current and next row.
type errBuf struct{ cur, next []int32 }

func newErrBuf(w, channels int) *errBuf {
	return &errBuf{cur: make([]int32, (w+2)*channels), next: make([]int32, (w+2)*channels)}
}

func (e *errBuf) advance() {
	e.cur, e.next = e.next, e.cur
	clear(e.next)
}

// spread distributes quantisation error with Floyd–Steinberg weights. Index i is x+1.
func (e *errBuf) spread(i, channels, c int, err int32) {
	e.cur[(i+1)*channels+c] += err * 7 / 16
	e.next[(i-1)*channels+c] += err * 3 / 16
	e.next[i*channels+c] += err * 5 / 16
	e.next[(i+1)*channels+c] += err * 1 / 16
}

func ditherColor(img *image.RGBA) *Bitmap {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	out := &Bitmap{Width: w, Height: h, Format: Color8, RowBytes: w, Data: make([]byte, w*h)}
	eb := newErrBuf(w, 3)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := img.PixOffset(x, y)
			i := x + 1
			var levels [3]int32
			for c := 0; c < 3; c++ {
				v := clamp(int32(img.Pix[p+c]) + eb.cur[i*3+c])
				level := (v + 42) / 85 // nearest of 0, 85, 170, 255
				levels[c] = level
				eb.spread(i, 3, c, v-level*85)
			}
			out.Data[y*w+x] = 0xC0 | byte(levels[0]<<4) | byte(levels[1]<<2) | byte(levels[2])
		}
		eb.advance()
	}
	return out
}

func ditherBW(img *image.RGBA) *Bitmap {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	rowBytes := (w + 7) / 8
	out := &Bitmap{Width: w, Height: h, Format: BW1, RowBytes: rowBytes, Data: make([]byte, rowBytes*h)}
	eb := newErrBuf(w, 1)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := img.PixOffset(x, y)
			lum := (299*int32(img.Pix[p]) + 587*int32(img.Pix[p+1]) + 114*int32(img.Pix[p+2])) / 1000
			v := clamp(lum + eb.cur[x+1])
			var q int32
			if v >= 128 {
				q = 255
				out.Data[y*rowBytes+x/8] |= 1 << (x % 8)
			}
			eb.spread(x+1, 1, 0, v-q)
		}
		eb.advance()
	}
	return out
}
