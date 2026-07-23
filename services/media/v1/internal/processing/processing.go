package processing

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"strings"

	_ "golang.org/x/image/webp"
	"golang.org/x/sync/semaphore"

	"github.com/soasurs/cordis/services/media/v1/config"
	"github.com/soasurs/cordis/services/media/v1/internal/objectstore"
	"github.com/soasurs/cordis/services/media/v1/internal/store"
)

// Processor validates source images and publishes the validated original
// representation under its immutable public key.
type Processor struct {
	stagingStore objectstore.ObjectStore
	publicStore  objectstore.ObjectStore
	cfg          config.MediaConfig
	limit        *semaphore.Weighted
}

// NewProcessor creates a bounded image processor.
func NewProcessor(
	stagingStore objectstore.ObjectStore,
	publicStore objectstore.ObjectStore,
	cfg config.MediaConfig,
) *Processor {
	return &Processor{
		stagingStore: stagingStore,
		publicStore:  publicStore,
		cfg:          cfg,
		limit:        semaphore.NewWeighted(cfg.ImageProcessingLimit()),
	}
}

// ProcessResult describes the validated source dimensions and published key.
type ProcessResult struct {
	Width        int32
	Height       int32
	PublishedKey string
}

// Process fully decodes the uploaded image and copies the original bytes to
// its immutable public key. The frontend owns crop and compression.
func (p *Processor) Process(ctx context.Context, asset *store.Asset) (*ProcessResult, error) {
	processCtx, cancel := context.WithTimeout(ctx, p.cfg.ImageProcessingTimeout())
	defer cancel()
	if err := p.limit.Acquire(processCtx, 1); err != nil {
		return nil, fmt.Errorf("acquire image processing capacity: %w", err)
	}
	defer p.limit.Release(1)

	reader, info, err := p.stagingStore.GetObject(processCtx, asset.StagingKey)
	if err != nil {
		return nil, fmt.Errorf("get staging object: %w", err)
	}
	defer reader.Close()

	if info.Size != asset.ExpectedSize {
		return nil, fmt.Errorf("image size %d does not match expected size %d", info.Size, asset.ExpectedSize)
	}
	if info.Size > p.cfg.MaxImageSize() {
		return nil, fmt.Errorf("image size %d exceeds limit %d", info.Size, p.cfg.MaxImageSize())
	}
	data, err := io.ReadAll(io.LimitReader(reader, asset.ExpectedSize+1))
	if err != nil {
		return nil, fmt.Errorf("read staging object: %w", err)
	}
	if int64(len(data)) != asset.ExpectedSize {
		return nil, fmt.Errorf("read image size %d does not match expected size %d", len(data), asset.ExpectedSize)
	}

	imageConfig, format, err := decodeImageConfig(data)
	if err != nil {
		return nil, fmt.Errorf("decode image config: %w", err)
	}
	actualContentType := imageContentType(format)
	if !strings.EqualFold(asset.ContentType, actualContentType) {
		return nil, fmt.Errorf(
			"decoded content type %q does not match expected content type %q",
			actualContentType,
			asset.ContentType,
		)
	}
	if imageAnimated(data, format) {
		return nil, errors.New("animated images are not supported")
	}
	if err := validateDimensions(imageConfig.Width, imageConfig.Height, p.cfg); err != nil {
		return nil, err
	}
	img, decodedFormat, err := decodeImage(data)
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	if decodedFormat != format {
		return nil, fmt.Errorf("decoded image format changed from %q to %q", format, decodedFormat)
	}
	if err := processCtx.Err(); err != nil {
		return nil, err
	}
	bounds := img.Bounds()
	origW := int32(bounds.Dx())
	origH := int32(bounds.Dy())
	if orientation := imageOrientation(data, format); orientation >= 5 {
		origW, origH = origH, origW
	}
	if err := validateDimensions(int(origW), int(origH), p.cfg); err != nil {
		return nil, err
	}
	publishedKey := asset.PublicKey()
	if err := p.publicStore.PutObject(
		processCtx,
		publishedKey,
		actualContentType,
		bytes.NewReader(data),
	); err != nil {
		return nil, fmt.Errorf("publish image: %w", err)
	}
	return &ProcessResult{
		Width:        origW,
		Height:       origH,
		PublishedKey: publishedKey,
	}, nil
}

func decodeImage(data []byte) (image.Image, string, error) {
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	switch format {
	case "jpeg", "png", "webp":
		return img, format, nil
	default:
		return nil, "", fmt.Errorf("unsupported image format: %s", format)
	}
}

func decodeImageConfig(data []byte) (image.Config, string, error) {
	imageConfig, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return image.Config{}, "", err
	}
	switch format {
	case "jpeg", "png", "webp":
		return imageConfig, format, nil
	default:
		return image.Config{}, "", fmt.Errorf("unsupported image format: %s", format)
	}
}

func validateDimensions(width, height int, cfg config.MediaConfig) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("image dimensions must be positive: %dx%d", width, height)
	}
	maxDim := int(cfg.MaxImageDim())
	if width > maxDim || height > maxDim {
		return fmt.Errorf("image dimensions %dx%d exceed limit %d", width, height, maxDim)
	}
	if pixels := int64(width) * int64(height); pixels > cfg.MaxPixels() {
		return fmt.Errorf("image pixel count %d exceeds limit %d", pixels, cfg.MaxPixels())
	}
	return nil
}

func imageContentType(format string) string {
	if format == "jpeg" {
		return "image/jpeg"
	}
	return "image/" + format
}

func imageAnimated(data []byte, format string) bool {
	switch format {
	case "png":
		return pngHasChunk(data, "acTL")
	case "webp":
		return webpHasAnimation(data)
	default:
		return false
	}
}

func pngHasChunk(data []byte, target string) bool {
	const signatureLength = 8
	if len(data) < signatureLength || !bytes.Equal(data[:signatureLength], []byte("\x89PNG\r\n\x1a\n")) {
		return false
	}
	for offset := signatureLength; offset+12 <= len(data); {
		length := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		if length < 0 || offset+12+length > len(data) {
			return false
		}
		if string(data[offset+4:offset+8]) == target {
			return true
		}
		offset += 12 + length
	}
	return false
}

func webpHasAnimation(data []byte) bool {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return false
	}
	for offset := 12; offset+8 <= len(data); {
		length := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		end := offset + 8 + length
		if length < 0 || end > len(data) {
			return false
		}
		switch string(data[offset : offset+4]) {
		case "ANIM", "ANMF":
			return true
		case "VP8X":
			if length > 0 && data[offset+8]&0x02 != 0 {
				return true
			}
		}
		offset = end + length%2
	}
	return false
}

func imageOrientation(data []byte, format string) int {
	switch format {
	case "jpeg":
		return jpegOrientation(data)
	case "png":
		return pngOrientation(data)
	case "webp":
		return webpOrientation(data)
	default:
		return 1
	}
}

func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	for offset := 2; offset+4 <= len(data); {
		if data[offset] != 0xff {
			offset++
			continue
		}
		for offset < len(data) && data[offset] == 0xff {
			offset++
		}
		if offset >= len(data) {
			break
		}
		marker := data[offset]
		offset++
		if marker == 0xd9 || marker == 0xda {
			break
		}
		if marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			continue
		}
		if offset+2 > len(data) {
			break
		}
		length := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		if length < 2 || offset+length > len(data) {
			break
		}
		if marker == 0xe1 {
			if orientation := exifOrientation(data[offset+2 : offset+length]); orientation != 1 {
				return orientation
			}
		}
		offset += length
	}
	return 1
}

func pngOrientation(data []byte) int {
	const signatureLength = 8
	if len(data) < signatureLength || !bytes.Equal(data[:signatureLength], []byte("\x89PNG\r\n\x1a\n")) {
		return 1
	}
	for offset := signatureLength; offset+12 <= len(data); {
		length := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		if length < 0 || offset+12+length > len(data) {
			break
		}
		if string(data[offset+4:offset+8]) == "eXIf" {
			return exifOrientation(data[offset+8 : offset+8+length])
		}
		offset += 12 + length
	}
	return 1
}

func webpOrientation(data []byte) int {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return 1
	}
	for offset := 12; offset+8 <= len(data); {
		length := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		end := offset + 8 + length
		if length < 0 || end > len(data) {
			break
		}
		if string(data[offset:offset+4]) == "EXIF" {
			return exifOrientation(data[offset+8 : end])
		}
		offset = end + length%2
	}
	return 1
}

func exifOrientation(data []byte) int {
	data = bytes.TrimPrefix(data, []byte("Exif\x00\x00"))
	if len(data) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	if order.Uint16(data[2:4]) != 42 {
		return 1
	}
	ifdOffset := int(order.Uint32(data[4:8]))
	if ifdOffset < 0 || ifdOffset+2 > len(data) {
		return 1
	}
	count := int(order.Uint16(data[ifdOffset : ifdOffset+2]))
	for i := range count {
		offset := ifdOffset + 2 + i*12
		if offset+12 > len(data) {
			break
		}
		entry := data[offset : offset+12]
		if order.Uint16(entry[:2]) != 0x0112 || order.Uint16(entry[2:4]) != 3 {
			continue
		}
		if order.Uint32(entry[4:8]) != 1 {
			continue
		}
		orientation := int(order.Uint16(entry[8:10]))
		if orientation >= 1 && orientation <= 8 {
			return orientation
		}
	}
	return 1
}

func applyOrientation(src image.Image, orientation int) image.Image {
	if orientation <= 1 || orientation > 8 {
		return src
	}
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	dstWidth, dstHeight := width, height
	if orientation >= 5 {
		dstWidth, dstHeight = height, width
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dstWidth, dstHeight))
	for y := range dstHeight {
		for x := range dstWidth {
			sourceX, sourceY := orientedSourcePoint(x, y, width, height, orientation)
			dst.Set(x, y, src.At(bounds.Min.X+sourceX, bounds.Min.Y+sourceY))
		}
	}
	return dst
}

func orientedSourcePoint(x, y, width, height, orientation int) (int, int) {
	switch orientation {
	case 2:
		return width - 1 - x, y
	case 3:
		return width - 1 - x, height - 1 - y
	case 4:
		return x, height - 1 - y
	case 5:
		return y, x
	case 6:
		return y, height - 1 - x
	case 7:
		return width - 1 - y, height - 1 - x
	case 8:
		return width - 1 - y, x
	default:
		return x, y
	}
}
