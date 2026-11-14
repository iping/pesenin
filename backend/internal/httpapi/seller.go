package httpapi

import (
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"pesenin.product.biz.id/api/internal/db"
	"pesenin.product.biz.id/api/internal/images"
	"pesenin.product.biz.id/api/internal/service"
)

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	me, err := s.App.LoadMe(r.Context(), user(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	body, err := s.meBody(r.Context(), me)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, body)
}

func (s *Server) slugCheck(w http.ResponseWriter, r *http.Request) {
	slug, ok, reason, sugg, err := s.App.CheckSlug(r.Context(), r.URL.Query().Get("slug"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"slug": slug, "available": ok, "reason": reason, "suggestions": sugg})
}

func (s *Server) createBrand(w http.ResponseWriter, r *http.Request) {
	var raw struct {
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
		WA          string `json:"wa"`
	}
	if err := readJSON(r, &raw); err != nil {
		writeErr(w, err)
		return
	}
	brand, store, err := s.App.CreateBrand(r.Context(), user(r).ID, service.NewBrand{
		Name: raw.Name, Slug: raw.Slug, Description: raw.Description, WA: raw.WA,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"brand": brand, "store": s.storeBody(r.Context(), store)})
}

func (s *Server) patchBrand(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "brandId")
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, _, err := s.App.OwnedBrand(r.Context(), user(r).ID, id); err != nil {
		writeErr(w, err)
		return
	}
	var raw struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		WA          string `json:"wa"`
	}
	if err := readJSON(r, &raw); err != nil {
		writeErr(w, err)
		return
	}
	cur, err := s.App.Q.GetBrandByID(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	var wa *string
	if raw.WA != "" {
		wa = &raw.WA
	} else {
		wa = cur.WaNumber
	}
	b, err := s.App.Q.UpdateBrand(r.Context(), db.UpdateBrandParams{
		ID: id, Name: raw.Name, Description: raw.Description, WaNumber: wa, LogoUrl: cur.LogoUrl,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, b)
}

func (s *Server) listStores(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "brandId")
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, _, err := s.App.OwnedBrand(r.Context(), user(r).ID, id); err != nil {
		writeErr(w, err)
		return
	}
	rows, err := s.App.Q.ListStores(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	now := time.Now()
	statuses, err := s.App.StoreStatuses(r.Context(), rows, now)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := []any{}
	for _, st := range rows {
		out = append(out, storeJSON(st, statuses[st.ID]))
	}
	writeJSON(w, 200, out)
}

func (s *Server) patchStore(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "storeId")
	if err != nil {
		writeErr(w, err)
		return
	}
	var body struct {
		Name                      string  `json:"name"`
		Address                   string  `json:"address"`
		MapsURL                   *string `json:"mapsUrl"`
		IsOpen                    bool    `json:"isOpen"`
		AcceptPreordersWhenClosed bool    `json:"acceptPreordersWhenClosed"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	st, err := s.App.UpdateStore(r.Context(), user(r).ID, id, service.StorePatch{
		Name: body.Name, Address: body.Address, MapsURL: body.MapsURL,
		IsOpen: body.IsOpen, AcceptPreordersWhenClosed: body.AcceptPreordersWhenClosed,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, s.storeBody(r.Context(), st))
}

func (s *Server) listBanks(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "brandId")
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, _, err := s.App.OwnedBrand(r.Context(), user(r).ID, id); err != nil {
		writeErr(w, err)
		return
	}
	rows, err := s.App.Q.ListBanks(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, rows)
}

func (s *Server) addBank(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "brandId")
	if err != nil {
		writeErr(w, err)
		return
	}
	var raw struct {
		BankCode, AccountNumber, HolderName string
		IsDefault                           bool
	}
	if err := readJSON(r, &raw); err != nil {
		writeErr(w, err)
		return
	}
	b, err := s.App.AddBank(r.Context(), user(r).ID, id, service.BankIn{
		BankCode: raw.BankCode, AccountNumber: raw.AccountNumber, HolderName: raw.HolderName, IsDefault: raw.IsDefault,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, b)
}

func (s *Server) listProducts(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "brandId")
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, _, err := s.App.OwnedBrand(r.Context(), user(r).ID, id); err != nil {
		writeErr(w, err)
		return
	}
	rows, err := s.App.Q.ListProducts(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	vars, _ := s.App.Q.ListVariantsByBrand(r.Context(), id)
	by := map[uuid.UUID][]db.ProductVariant{}
	for _, v := range vars {
		by[v.ProductID] = append(by[v.ProductID], v)
	}
	out := []any{}
	for _, p := range rows {
		item := map[string]any{
			"id": p.ID, "name": p.Name, "description": p.Description, "price": p.BasePrice,
			"active": p.Active, "variants": by[p.ID],
		}
		applyPhoto(item, p.PhotoUrl)
		out = append(out, item)
	}
	writeJSON(w, 200, out)
}

func (s *Server) saveProduct(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "brandId")
	if err != nil {
		writeErr(w, err)
		return
	}
	p, err := s.readProduct(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	row, err := s.App.SaveProduct(r.Context(), user(r).ID, id, nil, p)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, row)
}

func (s *Server) patchProduct(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "productId")
	if err != nil {
		writeErr(w, err)
		return
	}
	cur, err := s.App.Q.GetProduct(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	p, err := s.readProduct(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	row, err := s.App.SaveProduct(r.Context(), user(r).ID, cur.BrandID, &id, p)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, row)
}

func (s *Server) readProduct(r *http.Request) (service.ProductIn, error) {
	var raw struct {
		Name        string              `json:"name"`
		Description string              `json:"description"`
		Price       int64               `json:"price"`
		Active      *bool               `json:"active"`
		Variants    []service.VariantIn `json:"variants"`
	}
	if err := readJSON(r, &raw); err != nil {
		return service.ProductIn{}, err
	}
	active := true
	if raw.Active != nil {
		active = *raw.Active
	}
	return service.ProductIn{Name: raw.Name, Description: raw.Description, Price: raw.Price, Active: active, Variants: raw.Variants}, nil
}

func (s *Server) productPhoto(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "productId")
	if err != nil {
		writeErr(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, images.MaxUploadBytes)
	if err := r.ParseMultipartForm(images.MaxUploadBytes); err != nil {
		if tooLarge(err) {
			writeErr(w, service.E(400, "bad_file", images.ErrTooLarge.Message))
			return
		}
		writeErr(w, service.E(400, "bad_file", "Foto belum kebaca."))
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, service.E(400, "bad_file", "Pilih fotonya dulu."))
		return
	}
	defer f.Close()
	mime := hdr.Header.Get("Content-Type")
	if mime == "" || mime == "application/octet-stream" {
		mime = sniff(hdr.Filename)
	}
	p, err := s.App.SaveProductPhoto(r.Context(), user(r).ID, id, f, mime)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := map[string]any{}
	applyPhoto(out, p.PhotoUrl)
	writeJSON(w, 200, out)
}

func (s *Server) listCharges(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "brandId")
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, _, err := s.App.OwnedBrand(r.Context(), user(r).ID, id); err != nil {
		writeErr(w, err)
		return
	}
	rows, err := s.App.Q.ListCharges(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, rows)
}

func (s *Server) saveCharge(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "brandId")
	if err != nil {
		writeErr(w, err)
		return
	}
	in, err := readCharge(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	row, err := s.App.SaveCharge(r.Context(), user(r).ID, id, nil, in)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, row)
}

func (s *Server) patchCharge(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "chargeId")
	if err != nil {
		writeErr(w, err)
		return
	}
	cur, err := s.App.Q.GetCharge(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	in, err := readCharge(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	row, err := s.App.SaveCharge(r.Context(), user(r).ID, cur.BrandID, &id, in)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, row)
}

func readCharge(r *http.Request) (service.ChargeIn, error) {
	var raw struct {
		Name    string   `json:"name"`
		Amount  int64    `json:"amount"`
		Methods []string `json:"methods"`
		Active  *bool    `json:"active"`
	}
	if err := readJSON(r, &raw); err != nil {
		return service.ChargeIn{}, err
	}
	active := true
	if raw.Active != nil {
		active = *raw.Active
	}
	return service.ChargeIn{Name: raw.Name, Amount: raw.Amount, Methods: raw.Methods, Active: active}, nil
}

func (s *Server) listTemplates(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "brandId")
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, _, err := s.App.OwnedBrand(r.Context(), user(r).ID, id); err != nil {
		writeErr(w, err)
		return
	}
	rows, err := s.App.Q.ListTemplates(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, rows)
}

func (s *Server) putTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "brandId")
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, _, err := s.App.OwnedBrand(r.Context(), user(r).ID, id); err != nil {
		writeErr(w, err)
		return
	}
	var raw struct {
		Kind string `json:"kind"`
		Body string `json:"body"`
	}
	if err := readJSON(r, &raw); err != nil {
		writeErr(w, err)
		return
	}
	row, err := s.App.Q.UpsertTemplate(r.Context(), db.UpsertTemplateParams{BrandID: id, Kind: raw.Kind, Body: raw.Body})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, row)
}

func (s *Server) listCustomers(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "brandId")
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, _, err := s.App.OwnedBrand(r.Context(), user(r).ID, id); err != nil {
		writeErr(w, err)
		return
	}
	rows, err := s.App.Q.ListCustomers(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := []any{}
	for _, c := range rows {
		out = append(out, map[string]any{
			"id": c.ID, "name": c.Name, "wa": c.WaNumberE164, "ordersCount": c.OrdersCount,
			"totalSpent": c.TotalSpent, "lastOrderAt": tsP(c.LastOrderAt),
		})
	}
	writeJSON(w, 200, out)
}

func (s *Server) listBatches(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "brandId")
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, _, err := s.App.OwnedBrand(r.Context(), user(r).ID, id); err != nil {
		writeErr(w, err)
		return
	}
	rows, err := s.App.Q.ListBatchesByBrand(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := []any{}
	for _, b := range rows {
		items, _ := s.App.Q.ListBatchItems(r.Context(), b.ID)
		out = append(out, batchJSON(b, items, ""))
	}
	writeJSON(w, 200, out)
}

func (s *Server) createBatch(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "brandId")
	if err != nil {
		writeErr(w, err)
		return
	}
	in, err := readBatch(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	b, err := s.App.CreateBatch(r.Context(), user(r).ID, id, in)
	if err != nil {
		writeErr(w, err)
		return
	}
	items, _ := s.App.Q.ListBatchItems(r.Context(), b.ID)
	writeJSON(w, 201, batchJSON(b, items, ""))
}

func (s *Server) getBatch(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "batchId")
	if err != nil {
		writeErr(w, err)
		return
	}
	b, err := s.App.Q.GetBatch(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, _, err := s.App.OwnedBrand(r.Context(), user(r).ID, b.BrandID); err != nil {
		writeErr(w, err)
		return
	}
	items, _ := s.App.Q.ListBatchItems(r.Context(), b.ID)
	writeJSON(w, 200, batchJSON(b, items, ""))
}

func (s *Server) patchBatch(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "batchId")
	if err != nil {
		writeErr(w, err)
		return
	}
	in, err := readBatch(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	b, err := s.App.UpdateBatch(r.Context(), user(r).ID, id, in)
	if err != nil {
		writeErr(w, err)
		return
	}
	items, _ := s.App.Q.ListBatchItems(r.Context(), b.ID)
	writeJSON(w, 200, batchJSON(b, items, ""))
}

func readBatch(r *http.Request) (service.BatchIn, error) {
	var raw struct {
		StoreID           uuid.UUID      `json:"storeId"`
		Title             string         `json:"title"`
		OpensAt           time.Time      `json:"opensAt"`
		ClosesAt          time.Time      `json:"closesAt"`
		ReadyAt           time.Time      `json:"readyAt"`
		PickupSlots       []service.Slot `json:"pickupSlots"`
		DeadlineMins      *int32         `json:"paymentDeadlineMins"`
		PaymentDeadlineAt *time.Time     `json:"paymentDeadlineAt"`
		QuotaTotal        *int32         `json:"quotaTotal"`
		AllowPickup       bool           `json:"allowPickup"`
		AllowDelivery     bool           `json:"allowDelivery"`
		Notes             string         `json:"notes"`
		Publish           bool           `json:"publish"`
		Items             []struct {
			ProductID uuid.UUID  `json:"productId"`
			VariantID *uuid.UUID `json:"variantId"`
			Price     int64      `json:"price"`
			Quota     *int32     `json:"quota"`
		} `json:"items"`
	}
	if err := readJSON(r, &raw); err != nil {
		return service.BatchIn{}, err
	}
	in := service.BatchIn{
		StoreID: raw.StoreID, Title: raw.Title, OpensAt: raw.OpensAt, ClosesAt: raw.ClosesAt,
		ReadyAt: raw.ReadyAt, PickupSlots: raw.PickupSlots,
		DeadlineMins: raw.DeadlineMins, PaymentDeadlineAt: raw.PaymentDeadlineAt, QuotaTotal: raw.QuotaTotal, AllowPickup: raw.AllowPickup,
		AllowDelivery: raw.AllowDelivery, Notes: raw.Notes, Publish: raw.Publish,
	}
	for _, it := range raw.Items {
		in.Items = append(in.Items, service.BatchItemIn{ProductID: it.ProductID, VariantID: it.VariantID, Price: it.Price, Quota: it.Quota})
	}
	return in, nil
}

func (s *Server) openBatch(w http.ResponseWriter, r *http.Request) {
	s.flipBatch(w, r, true)
}
func (s *Server) closeBatch(w http.ResponseWriter, r *http.Request) {
	s.flipBatch(w, r, false)
}
func (s *Server) flipBatch(w http.ResponseWriter, r *http.Request, open bool) {
	id, err := pathUUID(r, "batchId")
	if err != nil {
		writeErr(w, err)
		return
	}
	b, err := s.App.SetBatchOpen(r.Context(), user(r).ID, id, open)
	if err != nil {
		writeErr(w, err)
		return
	}
	items, _ := s.App.Q.ListBatchItems(r.Context(), b.ID)
	writeJSON(w, 200, batchJSON(b, items, ""))
}

func (s *Server) duplicateBatch(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "batchId")
	if err != nil {
		writeErr(w, err)
		return
	}
	b, err := s.App.DuplicateBatch(r.Context(), user(r).ID, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	items, _ := s.App.Q.ListBatchItems(r.Context(), b.ID)
	writeJSON(w, 201, batchJSON(b, items, ""))
}

func (s *Server) recap(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "batchId")
	if err != nil {
		writeErr(w, err)
		return
	}
	include := r.URL.Query().Get("includeUnpaid") == "1"
	rec, err := s.App.Recap(r.Context(), user(r).ID, id, include)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, rec)
}

func (s *Server) recapCSV(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "batchId")
	if err != nil {
		writeErr(w, err)
		return
	}
	csv, err := s.App.RecapCSV(r.Context(), user(r).ID, id, r.URL.Query().Get("includeUnpaid") == "1")
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=rekap.csv")
	_, _ = io.WriteString(w, csv)
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	brandID, err := uuid.Parse(r.URL.Query().Get("brandId"))
	if err != nil {
		writeErr(w, service.E(400, "bad_id", "Pilih brand dulu."))
		return
	}
	if _, _, err := s.App.OwnedBrand(r.Context(), user(r).ID, brandID); err != nil {
		writeErr(w, err)
		return
	}
	orders, err := s.App.ListOrders(r.Context(), user(r).ID, brandID, "", nil, nil)
	if err != nil {
		writeErr(w, err)
		return
	}
	loc, _ := time.LoadLocation("Asia/Jakarta")
	now := time.Now().In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	var today, need int
	var omzet int64
	for _, o := range orders {
		if !o.CreatedAt.Before(start) {
			today++
		}
		if o.PaymentStatus == "proof_submitted" {
			need++
		}
		if o.PaymentStatus == "paid" && o.PaidAt.Valid && !o.PaidAt.Time.Before(start) {
			omzet += o.TotalDue
		}
	}
	batches, _ := s.App.Q.ListBatchesByBrand(r.Context(), brandID)
	var open any
	for _, b := range batches {
		if b.Status == "open" {
			items, _ := s.App.Q.ListBatchItems(r.Context(), b.ID)
			open = batchJSON(b, items, "")
			break
		}
	}
	writeJSON(w, 200, map[string]any{"todayOrders": today, "needCheck": need, "omzet": omzet, "openBatch": open})
}

func (s *Server) listOrders(w http.ResponseWriter, r *http.Request) {
	brandID, err := uuid.Parse(r.URL.Query().Get("brandId"))
	if err != nil {
		writeErr(w, service.E(400, "bad_id", "Pilih brand dulu."))
		return
	}
	var amount *int64
	if n, ok := queryInt(r, "amount"); ok {
		amount = &n
	}
	var batchID *uuid.UUID
	if raw := r.URL.Query().Get("batchId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeErr(w, service.E(400, "bad_id", "ID batch tidak valid."))
			return
		}
		batchID = &id
	}
	rows, err := s.App.ListOrders(r.Context(), user(r).ID, brandID, r.URL.Query().Get("status"), amount, batchID)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := []any{}
	for _, o := range rows {
		d, err := s.App.LoadOrder(r.Context(), o.ID)
		if err != nil {
			continue
		}
		out = append(out, orderJSON(d))
	}
	writeJSON(w, 200, out)
}

func (s *Server) getOrder(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "orderId")
	if err != nil {
		writeErr(w, err)
		return
	}
	d, err := s.App.LoadOrder(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, _, err := s.App.OwnedBrand(r.Context(), user(r).ID, d.Order.BrandID); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, orderJSON(d))
}

func (s *Server) markPaid(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "orderId")
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.App.MarkPaid(r.Context(), user(r).ID, id); err != nil {
		writeErr(w, err)
		return
	}
	d, err := s.App.LoadOrder(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, orderJSON(d))
}

func (s *Server) undoPaid(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "orderId")
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.App.UndoPaid(r.Context(), user(r).ID, id); err != nil {
		writeErr(w, err)
		return
	}
	d, _ := s.App.LoadOrder(r.Context(), id)
	writeJSON(w, 200, orderJSON(d))
}

func (s *Server) rejectProof(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "orderId")
	if err != nil {
		writeErr(w, err)
		return
	}
	var raw struct {
		Reason string `json:"reason"`
	}
	if err := readJSON(r, &raw); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.App.RejectProof(r.Context(), user(r).ID, id, raw.Reason); err != nil {
		writeErr(w, err)
		return
	}
	d, _ := s.App.LoadOrder(r.Context(), id)
	writeJSON(w, 200, orderJSON(d))
}

func (s *Server) cancelOrder(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "orderId")
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.App.CancelOrder(r.Context(), user(r).ID, id); err != nil {
		writeErr(w, err)
		return
	}
	d, _ := s.App.LoadOrder(r.Context(), id)
	writeJSON(w, 200, orderJSON(d))
}

func (s *Server) fulfil(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "orderId")
	if err != nil {
		writeErr(w, err)
		return
	}
	var raw struct {
		Status string `json:"status"`
	}
	if err := readJSON(r, &raw); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.App.SetFulfilment(r.Context(), user(r).ID, id, raw.Status); err != nil {
		writeErr(w, err)
		return
	}
	d, _ := s.App.LoadOrder(r.Context(), id)
	writeJSON(w, 200, orderJSON(d))
}

func (s *Server) waLink(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "orderId")
	if err != nil {
		writeErr(w, err)
		return
	}
	link, err := s.App.WALink(r.Context(), user(r).ID, id, r.URL.Query().Get("template"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]string{"url": link})
}
