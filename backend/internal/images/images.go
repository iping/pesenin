// Package images normalizes uploads into the sizes the UI actually shows.
//
// Phone cameras are not trusted: every accepted file is decoded, oriented,
// stripped of metadata, resized, and re-encoded. The encoder is libwebp via
// wazero, so the API binary stays CGO_ENABLED=0 and works on local disk and R2.
//
// Variant file names carry their pixel width so srcset can be built from the
// URL alone. The largest variant is the object stored in the database:
//
//	products/{id}/d1600-t480.webp   display bytes; thumb is t480.webp
//	proofs/{id}/f2400-d1440-t480.webp
//
// A legacy key such as proofs/{uuid}.jpg has no siblings.
package images

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"strconv"
	"strings"

	"github.com/disintegration/imaging"
	"github.com/gen2brain/webp"
	xdraw "golang.org/x/image/draw"
)

const (
	// MaxUploadBytes is the raw upload cap (phone JPEG / PNG / HEIC-converted).
	// Stored files are much smaller; see docs/images.md.
	MaxUploadBytes = 8 << 20
	// MaxPixels rejects decompression bombs before the full bitmap is kept.
	MaxPixels = 40_000_000

	ProofFullEdge       = 2400
	ProofFullQuality    = 86
	ProofDisplayEdge    = 1440
	ProofDisplayQuality = 82
	ProofThumbEdge      = 480
	ProofThumbQuality   = 74

	ProductDisplayEdge    = 1600
	ProductDisplayQuality = 80
	ProductThumbEdge      = 480
	ProductThumbQuality   = 74
)

// Kind selects the variant ladder.
type Kind int

const (
	Proof Kind = iota
	Product
)

// FileError is a client-facing upload failure.
type FileError struct {
	Status  int
	Code    string
	Message string
}

func (e *FileError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

var (
	ErrTooLarge = &FileError{Status: 400, Code: "bad_file", Message: "Ukuran foto maksimal 8 MB."}
	ErrEmpty    = &FileError{Status: 400, Code: "bad_file", Message: "Pilih fotonya dulu ya."}
	ErrType     = &FileError{Status: 400, Code: "bad_file", Message: "Kirim foto JPG, PNG, WebP, atau GIF ya."}
	ErrFormat   = &FileError{Status: 400, Code: "bad_file", Message: "Format HEIC/AVIF belum bisa diproses di server. Ubah ke JPG dulu ya."}
	ErrDecode   = &FileError{Status: 400, Code: "bad_file", Message: "Fotonya belum kebaca. Coba foto lain ya."}
	ErrPixels   = &FileError{Status: 400, Code: "bad_file", Message: "Resolusi fotonya terlalu besar."}
)

type slot struct {
	role    string
	code    byte
	maxEdge int
	quality int
}

// Largest first so a smaller slot is skipped when it would not shrink the image.
var proofSlots = []slot{
	{"full", 'f', ProofFullEdge, ProofFullQuality},
	{"display", 'd', ProofDisplayEdge, ProofDisplayQuality},
	{"thumb", 't', ProofThumbEdge, ProofThumbQuality},
}

var productSlots = []slot{
	{"display", 'd', ProductDisplayEdge, ProductDisplayQuality},
	{"thumb", 't', ProductThumbEdge, ProductThumbQuality},
}

// Variant is one encoded size.
type Variant struct {
	Role    string
	Width   int
	Height  int
	Quality int
	Data    []byte
}

// Result is the set of files for one upload.
type Result struct {
	Variants []Variant
	MIME     string
}

// Blob is one object to store. Primary is the file whose key is saved in the database.
type Blob struct {
	Key     string
	Data    []byte
	MIME    string
	Primary bool
	Role    string
	Width   int
}

// ReadLimited reads an upload and enforces MaxUploadBytes.
func ReadLimited(r io.Reader, max int64) ([]byte, error) {
	if max <= 0 {
		max = MaxUploadBytes
	}
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, ErrDecode
	}
	if len(b) == 0 {
		return nil, ErrEmpty
	}
	if int64(len(b)) > max {
		return nil, ErrTooLarge
	}
	return b, nil
}

// AllowedDeclared reports whether the browser-supplied Content-Type may
// accompany an image. The bytes are sniffed separately; this only rejects
// types that are never photos (SVG, HTML, PDF).
func AllowedDeclared(mime string) bool {
	mime = strings.ToLower(strings.TrimSpace(mime))
	if i := strings.Index(mime, ";"); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	switch mime {
	case "", "application/octet-stream", "binary/octet-stream",
		"image/jpeg", "image/jpg", "image/pjpeg",
		"image/png", "image/x-png",
		"image/webp",
		"image/gif",
		"image/heic", "image/heif", "image/heic-sequence", "image/heif-sequence":
		return true
	default:
		return false
	}
}

// Process decodes data, applies JPEG orientation, drops metadata, and
// encodes the variants for kind. Images smaller than a slot are not upscaled.
func Process(data []byte, kind Kind) (Result, error) {
	img, err := decode(data)
	if err != nil {
		return Result{}, err
	}
	if kind == Proof {
		img = flatten(img, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	} else {
		img = toNRGBA(img)
	}
	if err := checkPixels(img.Bounds().Dx(), img.Bounds().Dy()); err != nil {
		return Result{}, err
	}
	slots := productSlots
	if kind == Proof {
		slots = proofSlots
	}
	seen := map[[2]int]bool{}
	var variants []Variant
	for _, s := range slots {
		fitted := fit(img, s.maxEdge)
		b := fitted.Bounds()
		w, h := b.Dx(), b.Dy()
		key := [2]int{w, h}
		if seen[key] {
			continue
		}
		seen[key] = true
		encoded, err := encodeWebP(fitted, s.quality)
		if err != nil {
			return Result{}, err
		}
		variants = append(variants, Variant{
			Role: s.role, Width: w, Height: h, Quality: s.quality, Data: encoded,
		})
	}
	if len(variants) == 0 {
		return Result{}, ErrDecode
	}
	return Result{Variants: variants, MIME: "image/webp"}, nil
}

// Blobs returns the objects to write under dir (no trailing slash).
func (r Result) Blobs(dir string) []Blob {
	if len(r.Variants) == 0 {
		return nil
	}
	dir = strings.Trim(dir, "/")
	name := canonicalName(r.Variants)
	out := make([]Blob, 0, len(r.Variants))
	out = append(out, Blob{
		Key: dir + "/" + name, Data: r.Variants[0].Data, MIME: r.MIME,
		Primary: true, Role: r.Variants[0].Role, Width: r.Variants[0].Width,
	})
	for _, v := range r.Variants[1:] {
		out = append(out, Blob{
			Key:  dir + "/" + codeOf(v.Role) + strconv.Itoa(v.Width) + ".webp",
			Data: v.Data, MIME: r.MIME, Role: v.Role, Width: v.Width,
		})
	}
	return out
}

func canonicalName(vs []Variant) string {
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = codeOf(v.Role) + strconv.Itoa(v.Width)
	}
	return strings.Join(parts, "-") + ".webp"
}

func codeOf(role string) string {
	switch role {
	case "full":
		return "f"
	case "display":
		return "d"
	case "thumb":
		return "t"
	default:
		return "d"
	}
}

// View is the set of URLs a page needs for one stored image.
type View struct {
	Src       string
	Full      string
	Display   string
	Thumb     string
	Srcset    string
	Width     int
	FullWidth int
}

// Describe parses a stored object key or a /api/v1/media/... URL.
// Legacy single files come back with Src set and Srcset empty.
func Describe(ref string) View {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return View{}
	}
	path := ref
	if !strings.HasPrefix(path, "/") && !strings.Contains(path, "://") {
		path = "/api/v1/media/" + strings.TrimPrefix(path, "/")
	}
	if q := strings.Index(path, "?"); q >= 0 {
		path = path[:q]
	}
	slash := strings.LastIndex(path, "/")
	if slash < 0 {
		return View{Src: path, Full: path, Display: path, Thumb: path}
	}
	dir, file := path[:slash+1], path[slash+1:]
	dot := strings.LastIndex(file, ".")
	if dot <= 0 {
		return View{Src: path, Full: path, Display: path, Thumb: path}
	}
	ext := strings.ToLower(file[dot+1:])
	if ext != "webp" && ext != "jpg" && ext != "jpeg" {
		return View{Src: path, Full: path, Display: path, Thumb: path}
	}
	parts := splitName(file[:dot])
	if len(parts) == 0 {
		return View{Src: path, Full: path, Display: path, Thumb: path}
	}
	urlOf := func(i int) string {
		if i == 0 {
			return path
		}
		p := parts[i]
		return dir + string(p.code) + strconv.Itoa(p.width) + "." + ext
	}
	v := View{Src: path, Width: parts[0].width}
	type cand struct {
		url string
		w   int
	}
	var cands []cand
	for i, p := range parts {
		u := urlOf(i)
		switch p.code {
		case 'f':
			v.Full = u
			v.FullWidth = p.width
		case 'd':
			v.Display = u
		case 't':
			v.Thumb = u
		}
		cands = append(cands, cand{u, p.width})
	}
	if v.Full == "" {
		v.Full = v.Src
	}
	if v.Display == "" {
		v.Display = v.Src
	}
	if v.Thumb == "" {
		v.Thumb = v.Display
	}
	if v.FullWidth == 0 {
		v.FullWidth = v.Width
	}
	if len(cands) > 1 {
		// insertion sort, ascending width
		for i := 1; i < len(cands); i++ {
			j := i
			for j > 0 && cands[j].w < cands[j-1].w {
				cands[j], cands[j-1] = cands[j-1], cands[j]
				j--
			}
		}
		b := strings.Builder{}
		for i, c := range cands {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(c.url)
			b.WriteByte(' ')
			b.WriteString(strconv.Itoa(c.w))
			b.WriteByte('w')
		}
		v.Srcset = b.String()
	}
	return v
}

type namePart struct {
	code  byte
	width int
}

func splitName(stem string) []namePart {
	if stem == "" || len(stem) > 80 {
		return nil
	}
	raw := strings.Split(stem, "-")
	out := make([]namePart, 0, len(raw))
	for _, p := range raw {
		if len(p) < 2 || len(p) > 6 {
			return nil
		}
		switch p[0] {
		case 'f', 'd', 't':
		default:
			return nil
		}
		n, err := strconv.Atoi(p[1:])
		if err != nil || n < 1 || n > 10000 {
			return nil
		}
		out = append(out, namePart{code: p[0], width: n})
	}
	return out
}

func decode(data []byte) (*image.NRGBA, error) {
	switch sniff(data) {
	case "jpeg":
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, ErrDecode
		}
		if err := checkPixels(cfg.Width, cfg.Height); err != nil {
			return nil, err
		}
		img, err := imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
		if err != nil {
			return nil, ErrDecode
		}
		return toNRGBA(img), nil
	case "png":
		cfg, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, ErrDecode
		}
		if err := checkPixels(cfg.Width, cfg.Height); err != nil {
			return nil, err
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, ErrDecode
		}
		return toNRGBA(img), nil
	case "gif":
		cfg, err := gif.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, ErrDecode
		}
		if err := checkPixels(cfg.Width, cfg.Height); err != nil {
			return nil, err
		}
		img, err := gif.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, ErrDecode
		}
		return toNRGBA(img), nil
	case "webp":
		cfg, err := webp.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, ErrDecode
		}
		if err := checkPixels(cfg.Width, cfg.Height); err != nil {
			return nil, err
		}
		img, err := webp.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, ErrDecode
		}
		return toNRGBA(img), nil
	case "heif":
		return nil, ErrFormat
	default:
		return nil, ErrType
	}
}

func sniff(b []byte) string {
	if len(b) >= 3 && b[0] == 0xff && b[1] == 0xd8 && b[2] == 0xff {
		return "jpeg"
	}
	if len(b) >= 8 && bytes.Equal(b[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return "png"
	}
	if len(b) >= 6 && (bytes.Equal(b[:6], []byte("GIF87a")) || bytes.Equal(b[:6], []byte("GIF89a"))) {
		return "gif"
	}
	if len(b) >= 12 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")) {
		return "webp"
	}
	if isHEIF(b) {
		return "heif"
	}
	return ""
}

func isHEIF(b []byte) bool {
	if len(b) < 12 || string(b[4:8]) != "ftyp" {
		return false
	}
	n := int(uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3]))
	if n < 16 || n > len(b) {
		n = len(b)
	}
	if n > 64 {
		n = 64
	}
	brands := string(b[8:n])
	for _, brand := range []string{"heic", "heix", "hevc", "hevx", "heim", "heis", "mif1", "msf1", "avif"} {
		if strings.Contains(brands, brand) {
			return true
		}
	}
	return false
}

func checkPixels(w, h int) error {
	if w < 1 || h < 1 {
		return ErrDecode
	}
	if w > 12000 || h > 12000 {
		return ErrPixels
	}
	if int64(w)*int64(h) > MaxPixels {
		return ErrPixels
	}
	return nil
}

func toNRGBA(src image.Image) *image.NRGBA {
	if dst, ok := src.(*image.NRGBA); ok && dst.Bounds().Min == (image.Point{}) {
		return dst
	}
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst
}

func flatten(src image.Image, bg color.NRGBA) *image.NRGBA {
	b := src.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Over)
	return dst
}

func fit(src image.Image, maxEdge int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	long := w
	if h > long {
		long = h
	}
	if long <= maxEdge {
		return src
	}
	nw := int(float64(w)*float64(maxEdge)/float64(long) + 0.5)
	nh := int(float64(h)*float64(maxEdge)/float64(long) + 0.5)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewNRGBA(image.Rect(0, 0, nw, nh))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
	return dst
}

func encodeWebP(img image.Image, quality int) ([]byte, error) {
	if quality < 1 {
		quality = 1
	}
	if quality > 100 {
		quality = 100
	}
	var buf bytes.Buffer
	err := webp.Encode(&buf, img, webp.Options{
		Quality: quality,
		Method:  4,
	})
	if err != nil || buf.Len() < 12 {
		return nil, ErrDecode
	}
	return buf.Bytes(), nil
}
