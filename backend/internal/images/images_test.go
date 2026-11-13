package images

import (
	"bytes"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/gen2brain/webp"
)

func TestDescribeVariantsAndLegacy(t *testing.T) {
	v := Describe("proofs/abc/f2400-d1440-t480.webp")
	if v.Src != "/api/v1/media/proofs/abc/f2400-d1440-t480.webp" {
		t.Fatalf("src %s", v.Src)
	}
	if v.Full != v.Src || v.FullWidth != 2400 || v.Width != 2400 {
		t.Fatalf("full %+v", v)
	}
	if v.Display != "/api/v1/media/proofs/abc/d1440.webp" || v.Thumb != "/api/v1/media/proofs/abc/t480.webp" {
		t.Fatalf("siblings %+v", v)
	}
	want := "/api/v1/media/proofs/abc/t480.webp 480w, /api/v1/media/proofs/abc/d1440.webp 1440w, /api/v1/media/proofs/abc/f2400-d1440-t480.webp 2400w"
	if v.Srcset != want {
		t.Fatalf("srcset\n%s\n%s", v.Srcset, want)
	}

	p := Describe("/api/v1/media/products/abc/d1600-t480.webp")
	if p.Src != "/api/v1/media/products/abc/d1600-t480.webp" || p.Display != p.Src {
		t.Fatalf("product src %+v", p)
	}
	if p.Thumb != "/api/v1/media/products/abc/t480.webp" {
		t.Fatalf("thumb %s", p.Thumb)
	}
	if !strings.Contains(p.Srcset, "480w") || !strings.Contains(p.Srcset, "1600w") {
		t.Fatalf("product srcset %s", p.Srcset)
	}

	legacy := Describe("proofs/old.jpg")
	if legacy.Srcset != "" || legacy.Src != "/api/v1/media/proofs/old.jpg" || legacy.Full != legacy.Src {
		t.Fatalf("legacy %+v", legacy)
	}
	if Describe("").Src != "" {
		t.Fatal("empty")
	}
}

func TestRejectsNonImages(t *testing.T) {
	if _, err := Process([]byte("bukti"), Proof); err != ErrType {
		t.Fatalf("fake jpeg: %v", err)
	}
	if _, err := Process([]byte("<svg xmlns='http://www.w3.org/2000/svg'/>"), Product); err != ErrType {
		t.Fatalf("svg: %v", err)
	}
	heic := make([]byte, 32)
	copy(heic[4:12], []byte("ftypheic"))
	if _, err := Process(heic, Proof); err != ErrFormat {
		t.Fatalf("heic: %v", err)
	}
	huge := bytes.Repeat([]byte{0}, MaxUploadBytes+1)
	if _, err := ReadLimited(bytes.NewReader(huge), MaxUploadBytes); err != ErrTooLarge {
		t.Fatalf("size: %v", err)
	}
	if _, err := ReadLimited(bytes.NewReader(nil), MaxUploadBytes); err != ErrEmpty {
		t.Fatalf("empty: %v", err)
	}
	if AllowedDeclared("image/svg+xml") || AllowedDeclared("text/html") {
		t.Fatal("declared type should reject svg and html")
	}
	if !AllowedDeclared("image/jpeg") || !AllowedDeclared("image/heic") || !AllowedDeclared("") || !AllowedDeclared("application/octet-stream") {
		t.Fatal("declared type should allow camera uploads")
	}
}

func TestRejectsPixelBomb(t *testing.T) {
	// PNG IHDR claiming 10000x10000. DecodeConfig must fail the pixel cap
	// before the bitmap is expanded. CRC is required by image/png.
	pngBytes := hugePNG(10000, 10000)
	_, err := Process(pngBytes, Product)
	if err != ErrPixels {
		t.Fatalf("bomb: %v", err)
	}
}

func TestProofKeepsSmallTextAndStripsMetadata(t *testing.T) {
	const w, h = 1080, 1920
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 255
		if i%4 == 3 {
			img.Pix[i] = 255
		}
	}
	// 4px-per-cell digits, about 20px tall: the size of an account number
	// on a 1080-wide banking screenshot.
	drawDigits(img, 80, 200, 4, "1234567890")
	raw := encodeJPEG(t, img, 95)
	raw = insertCOM(raw, "GPSLAT:6.2088")
	res, err := Process(raw, Proof)
	if err != nil {
		t.Fatal(err)
	}
	if res.MIME != "image/webp" {
		t.Fatalf("mime %s", res.MIME)
	}
	full := res.Variants[0]
	if full.Role != "full" || full.Width != w || full.Height != h {
		t.Fatalf("full variant %+v", full)
	}
	if bytes.Contains(full.Data, []byte("GPSLAT")) || bytes.Contains(full.Data, []byte("Exif")) {
		t.Fatal("metadata survived re-encode")
	}
	got := decodeWebP(t, full.Data)
	// The first glyph of "1" is a vertical stroke at x=84..87. Sample its
	// core, and the white row above the line of digits.
	dark := luminance(got, 85, 208)
	light := luminance(got, 85, 180)
	if dark > 80 || light < 200 {
		t.Fatalf("text contrast lost: dark %d light %d", dark, light)
	}
	if len(full.Data) > 900_000 {
		t.Fatalf("screenshot full is %d bytes, over the proof budget", len(full.Data))
	}
	t.Logf("screenshot jpeg %d bytes (%dx%d) -> full webp %d (%dx%d q%d)", len(raw), w, h, len(full.Data), full.Width, full.Height, full.Quality)
	blobs := res.Blobs("proofs/id")
	if len(blobs) < 2 || !blobs[0].Primary || !strings.HasPrefix(blobs[0].Key, "proofs/id/f1080-") {
		t.Fatalf("blobs %+v", blobKeys(blobs))
	}
}

func TestJPEGOrientation(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 40, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			c := color.NRGBA{R: 220, A: 255}
			if x >= 20 {
				c = color.NRGBA{B: 220, A: 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	raw := encodeJPEG(t, img, 95)
	raw = insertOrientation(raw, 6) // 90° CW: left (red) becomes top
	res, err := Process(raw, Product)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeWebP(t, res.Variants[0].Data)
	if got.Bounds().Dx() != 20 || got.Bounds().Dy() != 40 {
		t.Fatalf("oriented size %dx%d", got.Bounds().Dx(), got.Bounds().Dy())
	}
	top := got.NRGBAAt(10, 4)
	bottom := got.NRGBAAt(10, 36)
	if top.R < 180 || top.B > 40 {
		t.Fatalf("top pixel not red after rotate: %+v", top)
	}
	if bottom.B < 180 || bottom.R > 40 {
		t.Fatalf("bottom pixel not blue after rotate: %+v", bottom)
	}
	if bytes.Contains(res.Variants[0].Data, []byte("Exif")) {
		t.Fatal("exif kept")
	}
}

func TestProductAndProofLadders(t *testing.T) {
	photo := smoothPhoto(2000, 1500)
	raw := encodeJPEG(t, photo, 90)
	res, err := Process(raw, Product)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Variants) != 2 {
		t.Fatalf("variants %d", len(res.Variants))
	}
	if res.Variants[0].Role != "display" || res.Variants[0].Width != 1600 || res.Variants[0].Height != 1200 {
		t.Fatalf("display %+v", res.Variants[0])
	}
	if res.Variants[1].Role != "thumb" || res.Variants[1].Width != 480 {
		t.Fatalf("thumb %+v", res.Variants[1])
	}
	if res.Variants[0].Quality != ProductDisplayQuality || res.Variants[1].Quality != ProductThumbQuality {
		t.Fatalf("quality %+v %+v", res.Variants[0].Quality, res.Variants[1].Quality)
	}
	before := len(raw)
	t.Logf("smooth photo jpeg %d bytes (%dx%d q90) -> display webp %d (%dx%d q%d) thumb %d (%dx%d q%d)",
		before, 2000, 1500,
		len(res.Variants[0].Data), res.Variants[0].Width, res.Variants[0].Height, res.Variants[0].Quality,
		len(res.Variants[1].Data), res.Variants[1].Width, res.Variants[1].Height, res.Variants[1].Quality)

	// Portrait camera frame, larger than the proof cap, so full must shrink to 2400.
	shot := smoothPhoto(3024, 4032)
	shotJPEG := encodeJPEG(t, shot, 92)
	proof, err := Process(shotJPEG, Proof)
	if err != nil {
		t.Fatal(err)
	}
	if proof.Variants[0].Role != "full" || proof.Variants[0].Height != ProofFullEdge {
		t.Fatalf("proof full %+v", proof.Variants[0])
	}
	if proof.Variants[0].Width != 1800 { // 3024 * 2400 / 4032 = 1800
		t.Fatalf("proof width %d", proof.Variants[0].Width)
	}
	var display, thumb *Variant
	for i := range proof.Variants {
		switch proof.Variants[i].Role {
		case "display":
			display = &proof.Variants[i]
		case "thumb":
			thumb = &proof.Variants[i]
		}
	}
	if display == nil || display.Height != ProofDisplayEdge || thumb == nil || thumb.Height != ProofThumbEdge {
		t.Fatalf("proof ladder %+v", proof.Variants)
	}
	t.Logf("camera jpeg %d bytes (3024x4032 q92) -> full %d (%dx%d q%d) display %d (%dx%d) thumb %d (%dx%d)",
		len(shotJPEG),
		len(proof.Variants[0].Data), proof.Variants[0].Width, proof.Variants[0].Height, proof.Variants[0].Quality,
		len(display.Data), display.Width, display.Height,
		len(thumb.Data), thumb.Width, thumb.Height)

	pngBytes := encodePNG(t, smoothPhoto(800, 800))
	fromPNG, err := Process(pngBytes, Product)
	if err != nil {
		t.Fatal(err)
	}
	if fromPNG.MIME != "image/webp" || fromPNG.Variants[0].Width != 800 {
		t.Fatalf("png input %+v", fromPNG.Variants[0])
	}
	t.Logf("png %d bytes (800x800) -> display webp %d thumb %d", len(pngBytes), len(fromPNG.Variants[0].Data), len(fromPNG.Variants[len(fromPNG.Variants)-1].Data))

	noise := noisyPhoto(1600, 1200)
	noiseJPEG := encodeJPEG(t, noise, 90)
	noisy, err := Process(noiseJPEG, Product)
	if err != nil {
		t.Fatal(err)
	}
	// Pure high-frequency noise is the size ceiling. Real photos sit well below it.
	// Quality is not lowered to chase that ceiling, or text and food detail would suffer.
	t.Logf("noise stress jpeg %d bytes (1600x1200 q90) -> display webp %d (%dx%d q%d) thumb %d",
		len(noiseJPEG), len(noisy.Variants[0].Data), noisy.Variants[0].Width, noisy.Variants[0].Height, noisy.Variants[0].Quality,
		len(noisy.Variants[len(noisy.Variants)-1].Data))
}

func noisyPhoto(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			n := uint8((x * 13) ^ (y * 29) ^ ((x * y) >> 8))
			img.SetNRGBA(x, y, color.NRGBA{R: n, G: n ^ 90, B: 255 - n, A: 255})
		}
	}
	return img
}

func TestLogoJPEGSample(t *testing.T) {
	b, err := os.ReadFile("../../../frontend/static/brand/logo.jpg")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Process(b, Product)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("logo.jpg %d bytes -> display %d (%dx%d) thumb %d (%dx%d)",
		len(b), len(res.Variants[0].Data), res.Variants[0].Width, res.Variants[0].Height,
		len(res.Variants[1].Data), res.Variants[1].Width, res.Variants[1].Height)
	if len(res.Variants[0].Data) >= len(b) {
		t.Fatalf("display webp %d is not smaller than logo.jpg %d", len(res.Variants[0].Data), len(b))
	}
}

func TestRoundTripWebPInput(t *testing.T) {
	raw := encodeJPEG(t, smoothPhoto(320, 200), 85)
	once, err := Process(raw, Product)
	if err != nil {
		t.Fatal(err)
	}
	twice, err := Process(once.Variants[0].Data, Product)
	if err != nil {
		t.Fatal(err)
	}
	if twice.Variants[0].Width != 320 || twice.MIME != "image/webp" {
		t.Fatalf("webp input %+v", twice.Variants[0])
	}
}

func smoothPhoto(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(180 + (x*40)/w),
				G: uint8(90 + (y*80)/h),
				B: uint8(40 + ((x+y)*30)/(w+h)),
				A: 255,
			})
		}
	}
	// A darker "subject" so the frame is not a single gradient.
	for y := h / 5; y < h*3/5; y++ {
		for x := w / 5; x < w*3/5; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 214, G: 96, B: 42, A: 255})
		}
	}
	return img
}

func encodeJPEG(t *testing.T, img image.Image, q int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func decodeWebP(t *testing.T, b []byte) *image.NRGBA {
	t.Helper()
	img, err := webp.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return toNRGBA(img)
}

func luminance(img *image.NRGBA, x, y int) int {
	c := img.NRGBAAt(x, y)
	return (int(c.R)*3 + int(c.G)*6 + int(c.B)) / 10
}

func blobKeys(b []Blob) []string {
	out := make([]string, len(b))
	for i := range b {
		out[i] = b[i].Key
	}
	return out
}

// 3x5 digits, scaled. Thick enough that WebP q=86 keeps the stroke.
var digitBits = map[byte][5]string{
	'1': {"010", "110", "010", "010", "111"},
	'2': {"111", "001", "111", "100", "111"},
	'3': {"111", "001", "111", "001", "111"},
	'4': {"101", "101", "111", "001", "001"},
	'5': {"111", "100", "111", "001", "111"},
	'6': {"111", "100", "111", "101", "111"},
	'7': {"111", "001", "010", "010", "010"},
	'8': {"111", "101", "111", "101", "111"},
	'9': {"111", "101", "111", "001", "111"},
	'0': {"111", "101", "101", "101", "111"},
}

func drawDigits(img *image.NRGBA, x, y, scale int, s string) {
	cursor := x
	black := color.NRGBA{A: 255}
	for i := 0; i < len(s); i++ {
		glyph, ok := digitBits[s[i]]
		if !ok {
			continue
		}
		for gy, row := range glyph {
			for gx := 0; gx < len(row); gx++ {
				if row[gx] != '1' {
					continue
				}
				for dy := 0; dy < scale; dy++ {
					for dx := 0; dx < scale; dx++ {
						img.SetNRGBA(cursor+gx*scale+dx, y+gy*scale+dy, black)
					}
				}
			}
		}
		cursor += (3 + 1) * scale
	}
}

func insertCOM(jpegBytes []byte, text string) []byte {
	// JPEG COM segment, after SOI. Re-encoding must drop it.
	payload := append([]byte(text), 0)
	seg := []byte{0xff, 0xfe, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}
	seg = append(seg, payload...)
	out := make([]byte, 0, len(jpegBytes)+len(seg))
	out = append(out, jpegBytes[:2]...)
	out = append(out, seg...)
	out = append(out, jpegBytes[2:]...)
	return out
}

func insertOrientation(jpegBytes []byte, orientation byte) []byte {
	// Minimal big-endian TIFF EXIF with a single Orientation short.
	tiff := []byte{
		'M', 'M', 0x00, 0x2a,
		0x00, 0x00, 0x00, 0x08,
		0x00, 0x01,
		0x01, 0x12, 0x00, 0x03, 0x00, 0x00, 0x00, 0x01, 0x00, orientation, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}
	body := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xff, 0xe1, byte((len(body) + 2) >> 8), byte(len(body) + 2)}
	seg = append(seg, body...)
	out := make([]byte, 0, len(jpegBytes)+len(seg))
	out = append(out, jpegBytes[:2]...)
	out = append(out, seg...)
	out = append(out, jpegBytes[2:]...)
	return out
}

func hugePNG(w, h int) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	ihdr := make([]byte, 13)
	ihdr[0] = byte(w >> 24)
	ihdr[1] = byte(w >> 16)
	ihdr[2] = byte(w >> 8)
	ihdr[3] = byte(w)
	ihdr[4] = byte(h >> 24)
	ihdr[5] = byte(h >> 16)
	ihdr[6] = byte(h >> 8)
	ihdr[7] = byte(h)
	ihdr[8] = 8 // bit depth
	ihdr[9] = 2 // truecolor
	writeChunk(&buf, "IHDR", ihdr)
	// One empty IDAT is enough for DecodeConfig, which stops at IHDR.
	return buf.Bytes()
}

func writeChunk(buf *bytes.Buffer, kind string, data []byte) {
	var lenb [4]byte
	n := len(data)
	lenb[0] = byte(n >> 24)
	lenb[1] = byte(n >> 16)
	lenb[2] = byte(n >> 8)
	lenb[3] = byte(n)
	buf.Write(lenb[:])
	buf.WriteString(kind)
	buf.Write(data)
	sum := crc32.ChecksumIEEE(append([]byte(kind), data...))
	var c [4]byte
	c[0] = byte(sum >> 24)
	c[1] = byte(sum >> 16)
	c[2] = byte(sum >> 8)
	c[3] = byte(sum)
	buf.Write(c[:])
}
