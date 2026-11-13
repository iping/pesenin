package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	qrcode "github.com/skip2/go-qrcode"

	"pesenin.product.biz.id/api/internal/db"
	"pesenin.product.biz.id/api/internal/images"
	"pesenin.product.biz.id/api/internal/service"
	"pesenin.product.biz.id/api/internal/storage"
)

var bankCodes = []map[string]string{
	{"code": "BCA", "name": "BCA"},
	{"code": "BNI", "name": "BNI"},
	{"code": "BRI", "name": "BRI"},
	{"code": "MANDIRI", "name": "Mandiri"},
	{"code": "CIMB", "name": "CIMB Niaga"},
	{"code": "PERMATA", "name": "Permata"},
	{"code": "BSI", "name": "BSI"},
	{"code": "JAGO", "name": "Jago"},
	{"code": "SEABANK", "name": "SeaBank"},
	{"code": "DANA", "name": "DANA"},
	{"code": "OVO", "name": "OVO"},
	{"code": "GOPAY", "name": "GoPay"},
	{"code": "SHOPEEPAY", "name": "ShopeePay"},
}

func (s *Server) bankCodes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, bankCodes)
}

func (s *Server) vapidKey(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]string{"publicKey": s.App.Push.PublicKey()})
}

func (s *Server) publicBrand(w http.ResponseWriter, r *http.Request) {
	s.writePublic(w, r, chi.URLParam(r, "slug"), "")
}

func (s *Server) publicStore(w http.ResponseWriter, r *http.Request) {
	s.writePublic(w, r, chi.URLParam(r, "slug"), chi.URLParam(r, "store"))
}

func (s *Server) writePublic(w http.ResponseWriter, r *http.Request, slug, storeSlug string) {
	brand, err := s.App.Q.GetBrandBySlug(r.Context(), slug)
	if err != nil {
		writeErr(w, err)
		return
	}
	stores, err := s.App.Q.ListStores(r.Context(), brand.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	var store db.Store
	if storeSlug == "" {
		if len(stores) == 0 {
			writeErr(w, service.E(404, "not_found", "Toko belum ada."))
			return
		}
		store = stores[0]
	} else {
		st, err := s.App.Q.GetStoreBySlug(r.Context(), db.GetStoreBySlugParams{BrandID: brand.ID, Slug: storeSlug})
		if err != nil {
			writeErr(w, err)
			return
		}
		store = st
	}
	batches, err := s.App.Q.ListOpenBatchesByBrand(r.Context(), brand.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	var open []any
	for _, b := range batches {
		if b.StoreID != store.ID {
			continue
		}
		items, _ := s.App.Q.ListBatchItems(r.Context(), b.ID)
		open = append(open, s.batchWithPhotos(r, b, items, store.Timezone))
	}
	if open == nil {
		open = []any{}
	}
	charges, _ := s.App.Q.ListActiveCharges(r.Context(), brand.ID)
	chs := []any{}
	for _, c := range charges {
		chs = append(chs, map[string]any{"id": c.ID, "name": c.Name, "amount": c.AmountRp, "methods": c.FulfilmentMethods})
	}
	products, _ := s.App.Q.ListProducts(r.Context(), brand.ID)
	photos := map[string]any{}
	for _, p := range products {
		if p.PhotoUrl != nil {
			v := images.Describe(*p.PhotoUrl)
			photos[p.ID.String()] = v.Src
		}
	}
	writeJSON(w, 200, map[string]any{
		"brand": map[string]any{
			"name": brand.Name, "slug": brand.Slug, "description": brand.Description,
			"waNumber": brand.WaNumber, "logoUrl": brand.LogoUrl,
		},
		"store":     storeJSON(store),
		"batches":   open,
		"charges":   chs,
		"photos":    photos,
		"ordering":  store.IsOpen || store.AcceptPreordersWhenClosed,
		"serverNow": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) batchWithPhotos(r *http.Request, b db.PoBatch, items []db.BatchItem, tz string) map[string]any {
	view := batchJSON(b, items, tz)
	list := view["items"].([]any)
	for i, it := range items {
		p, err := s.App.Q.GetProduct(r.Context(), it.ProductID)
		m := list[i].(map[string]any)
		if err == nil {
			applyPhoto(m, p.PhotoUrl)
			m["description"] = p.Description
		}
		list[i] = m
	}
	view["items"] = list
	return view
}

func (s *Server) publicBatch(w http.ResponseWriter, r *http.Request) {
	b, err := s.App.Q.GetBatchByCode(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		writeErr(w, err)
		return
	}
	items, err := s.App.Q.ListBatchItems(r.Context(), b.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	brand, _ := s.App.Q.GetBrandByID(r.Context(), b.BrandID)
	store, _ := s.App.Q.GetStore(r.Context(), b.StoreID)
	charges, _ := s.App.Q.ListActiveCharges(r.Context(), b.BrandID)
	chs := []any{}
	for _, c := range charges {
		chs = append(chs, map[string]any{"name": c.Name, "amount": c.AmountRp, "methods": c.FulfilmentMethods})
	}
	writeJSON(w, 200, map[string]any{
		"batch":     s.batchWithPhotos(r, b, items, store.Timezone),
		"brand":     map[string]any{"name": brand.Name, "slug": brand.Slug, "waNumber": brand.WaNumber, "description": brand.Description},
		"store":     storeJSON(store),
		"charges":   chs,
		"ordering":  b.Status == "open" && (store.IsOpen || store.AcceptPreordersWhenClosed),
		"serverNow": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) quoteOrder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Method string             `json:"method"`
		Items  []service.CartItem `json:"items"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	q, err := s.App.Quote(r.Context(), chi.URLParam(r, "code"), body.Method, body.Items)
	if err != nil {
		writeErr(w, err)
		return
	}
	chs := []any{}
	for _, c := range q.Charges {
		chs = append(chs, map[string]any{"name": c.Name, "amount": c.AmountRp})
	}
	writeJSON(w, 200, map[string]any{"subtotal": q.Subtotal, "charges": chs, "extra": q.Extra, "baseTotal": q.Base})
}

func (s *Server) placeOrder(w http.ResponseWriter, r *http.Request) {
	var body service.PlaceOrder
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	if body.IdempotencyKey == "" {
		body.IdempotencyKey = r.Header.Get("Idempotency-Key")
	}
	o, err := s.App.PlaceOrder(r.Context(), chi.URLParam(r, "code"), body)
	if err != nil {
		writeErr(w, err)
		return
	}
	d, err := s.App.LoadOrder(r.Context(), o.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, orderJSON(d))
}

func (s *Server) publicOrder(w http.ResponseWriter, r *http.Request) {
	d, err := s.App.LoadOrderByToken(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, orderJSON(d))
}

func (s *Server) publicProof(w http.ResponseWriter, r *http.Request) {
	s.uploadProof(w, r, func(rc io.Reader, mime string) error {
		return s.App.SaveProof(r.Context(), chi.URLParam(r, "token"), rc, mime)
	})
}

func (s *Server) uploadProof(w http.ResponseWriter, r *http.Request, save func(io.Reader, string) error) {
	r.Body = http.MaxBytesReader(w, r.Body, images.MaxUploadBytes)
	if err := r.ParseMultipartForm(images.MaxUploadBytes); err != nil {
		if tooLarge(err) {
			writeErr(w, service.E(400, "bad_file", images.ErrTooLarge.Message))
			return
		}
		writeErr(w, service.E(400, "bad_file", "Bukti belum kebaca. Coba foto yang lebih kecil."))
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, service.E(400, "bad_file", "Pilih file bukti dulu."))
		return
	}
	defer f.Close()
	mime := hdr.Header.Get("Content-Type")
	if mime == "" || mime == "application/octet-stream" {
		mime = sniff(hdr.Filename)
	}
	if err := save(f, mime); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func sniff(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.HasSuffix(n, ".png"):
		return "image/png"
	case strings.HasSuffix(n, ".webp"):
		return "image/webp"
	case strings.HasSuffix(n, ".gif"):
		return "image/gif"
	case strings.HasSuffix(n, ".heic"), strings.HasSuffix(n, ".heif"):
		return "image/heic"
	default:
		return "image/jpeg"
	}
}

func tooLarge(err error) bool {
	var max *http.MaxBytesError
	return errors.As(err, &max) || (err != nil && strings.Contains(err.Error(), "too large"))
}

func mediaType(key string) string {
	switch {
	case strings.HasSuffix(key, ".png"):
		return "image/png"
	case strings.HasSuffix(key, ".webp"):
		return "image/webp"
	case strings.HasSuffix(key, ".gif"):
		return "image/gif"
	default:
		return "image/jpeg"
	}
}

func (s *Server) qrPNG(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("url")
	if raw == "" || !strings.HasPrefix(raw, s.App.Cfg.PublicOrigin) {
		writeErr(w, service.E(400, "bad_url", "QR cuma untuk link Pesenin."))
		return
	}
	size := 1024
	if n, err := strconv.Atoi(r.URL.Query().Get("size")); err == nil && n >= 256 && n <= 2048 {
		size = n
	}
	png, err := qrcode.Encode(raw, qrcode.Medium, size)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(png)
}

func (s *Server) media(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/api/v1/media/")
	rc, err := s.App.Blob.Open(r.Context(), key)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", mediaType(key))
	w.Header().Set("Cache-Control", storage.CacheControl(key))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, rc)
}
