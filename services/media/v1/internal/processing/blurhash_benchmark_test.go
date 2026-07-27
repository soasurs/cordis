package processing

import (
	"image"
	"image/color"
	"testing"

	"github.com/bbrks/go-blurhash"
)

func BenchmarkEncodeBlurhash(b *testing.B) {
	for _, size := range []struct {
		name   string
		width  int
		height int
	}{
		{name: "32x32", width: 32, height: 32},
		{name: "256x256", width: 256, height: 256},
		{name: "1024x768", width: 1024, height: 768},
		{name: "4096x4096", width: 4096, height: 4096},
	} {
		img := gradientImage(size.width, size.height)
		b.Run(size.name+"/downsampled", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if hash := encodeBlurhash(img, 1); hash == "" {
					b.Fatal("empty blurhash")
				}
			}
		})
		if size.width*size.height <= 1024*768 {
			b.Run(size.name+"/full_resolution", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					hash, err := blurhash.Encode(blurhashXComponents, blurhashYComponents, img)
					if err != nil || hash == "" {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func gradientImage(width, height int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x % 256),
				G: uint8(y % 256),
				B: uint8((x + y) % 256),
				A: 255,
			})
		}
	}
	return img
}
