package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"pesenin.product.biz.id/api/internal/clock"
	"pesenin.product.biz.id/api/internal/db"
	"pesenin.product.biz.id/api/internal/text"
)

func (a *App) CheckSlug(ctx context.Context, raw string) (slug string, available bool, reason string, suggestions []string, err error) {
	slug = text.Slug(raw)
	ok, reason := text.SlugOK(slug, false)
	if !ok {
		return slug, false, reason, nil, nil
	}
	taken, err := a.Q.SlugTaken(ctx, slug)
	if err != nil {
		return slug, false, "", nil, err
	}
	if !taken {
		return slug, true, "", nil, nil
	}
	suggestions = text.Suggest(slug, func(s string) bool {
		yes, e := a.Q.SlugTaken(ctx, s)
		return e != nil || yes
	})
	return slug, false, "Sudah dipakai.", suggestions, nil
}

type NewBrand struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	WA          string `json:"wa"`
}

func (a *App) CreateBrand(ctx context.Context, userID uuid.UUID, in NewBrand) (db.Brand, db.Store, error) {
	acc, err := a.Q.GetAccountByOwner(ctx, userID)
	if err != nil {
		return db.Brand{}, db.Store{}, err
	}
	n, err := a.Q.CountBrands(ctx, acc.ID)
	if err != nil {
		return db.Brand{}, db.Store{}, err
	}
	if int(n) >= int(acc.BrandSlots) {
		return db.Brand{}, db.Store{}, E(403, "slot_required", "Slot brand tambahan belum dibuka. Fase ini cuma 1 brand dan 1 toko.")
	}
	slug := text.Slug(in.Slug)
	if slug == "" {
		slug = text.Slug(in.Name)
	}
	if ok, msg := text.SlugOK(slug, false); !ok {
		return db.Brand{}, db.Store{}, E(400, "bad_slug", msg)
	}
	if taken, err := a.Q.SlugTaken(ctx, slug); err != nil || taken {
		if taken {
			return db.Brand{}, db.Store{}, E(409, "slug_taken", "Slug sudah dipakai. Coba yang lain ya.")
		}
		return db.Brand{}, db.Store{}, err
	}
	var wa *string
	if in.WA != "" {
		num, err := text.WhatsApp(in.WA)
		if err != nil {
			return db.Brand{}, db.Store{}, E(400, "bad_wa", "Nomor WhatsApp toko belum pas.")
		}
		wa = &num
	}
	var brand db.Brand
	var store db.Store
	err = a.tx(ctx, func(q *db.Queries) error {
		var err error
		brand, err = q.CreateBrand(ctx, db.CreateBrandParams{
			AccountID: acc.ID, Name: in.Name, Slug: slug, Description: in.Description, WaNumber: wa, Status: "active",
		})
		if err != nil {
			return err
		}
		store, err = q.CreateStore(ctx, db.CreateStoreParams{
			BrandID: brand.ID, Name: "Utama", Slug: "utama", IsOpen: true, AcceptPreordersWhenClosed: true, IsDefault: true,
		})
		if err != nil {
			return err
		}
		for _, pair := range []struct{ kind, body string }{
			{"confirmation", defaultConfirm},
			{"reminder", defaultRemind},
			{"ready", defaultReady},
		} {
			if _, err := q.UpsertTemplate(ctx, db.UpsertTemplateParams{BrandID: brand.ID, Kind: pair.kind, Body: pair.body}); err != nil {
				return err
			}
		}
		a.audit(ctx, q, &acc.ID, &userID, "brand.create", "brand", brand.ID.String(), nil, map[string]string{"slug": slug, "name": in.Name})
		return nil
	})
	return brand, store, err
}

type StorePatch struct {
	Name                      string
	Address                   string
	MapsURL                   *string
	IsOpen                    bool
	AcceptPreordersWhenClosed bool
}

func (a *App) UpdateStore(ctx context.Context, userID, storeID uuid.UUID, in StorePatch) (db.Store, error) {
	st, err := a.Q.GetStore(ctx, storeID)
	if err != nil {
		return db.Store{}, err
	}
	if _, _, err := a.OwnedBrand(ctx, userID, st.BrandID); err != nil {
		return db.Store{}, err
	}
	return a.Q.UpdateStore(ctx, db.UpdateStoreParams{
		ID: st.ID, Name: in.Name, Address: in.Address, MapsUrl: in.MapsURL,
		IsOpen: in.IsOpen, AcceptPreordersWhenClosed: in.AcceptPreordersWhenClosed,
	})
}

type BankIn struct {
	BankCode      string
	AccountNumber string
	HolderName    string
	IsDefault     bool
}

func (a *App) AddBank(ctx context.Context, userID, brandID uuid.UUID, in BankIn) (db.BankAccount, error) {
	if _, _, err := a.OwnedBrand(ctx, userID, brandID); err != nil {
		return db.BankAccount{}, err
	}
	n, err := a.Q.CountBanks(ctx, brandID)
	if err != nil {
		return db.BankAccount{}, err
	}
	if n >= 3 {
		return db.BankAccount{}, E(400, "bank_limit", "Maksimal 3 rekening.")
	}
	if in.BankCode == "" || in.AccountNumber == "" || in.HolderName == "" {
		return db.BankAccount{}, E(400, "bad_bank", "Lengkapi bank, nomor, dan nama pemilik.")
	}
	def := in.IsDefault || n == 0
	var bank db.BankAccount
	err = a.tx(ctx, func(q *db.Queries) error {
		if def {
			if err := q.ClearDefaultBank(ctx, brandID); err != nil {
				return err
			}
		}
		var err error
		bank, err = q.CreateBank(ctx, db.CreateBankParams{
			BrandID: brandID, BankCode: in.BankCode, AccountNumber: in.AccountNumber, HolderName: in.HolderName, IsDefault: def,
		})
		return err
	})
	return bank, err
}

type VariantIn struct {
	Name       string `json:"name"`
	PriceDelta int64  `json:"priceDelta"`
}

type ProductIn struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Price       int64       `json:"price"`
	Active      bool        `json:"active"`
	Variants    []VariantIn `json:"variants"`
}

func (a *App) SaveProduct(ctx context.Context, userID, brandID uuid.UUID, productID *uuid.UUID, in ProductIn) (db.Product, error) {
	if _, _, err := a.OwnedBrand(ctx, userID, brandID); err != nil {
		return db.Product{}, err
	}
	if in.Name == "" || in.Price < 0 {
		return db.Product{}, E(400, "bad_product", "Nama dan harga produk wajib diisi.")
	}
	var p db.Product
	err := a.tx(ctx, func(q *db.Queries) error {
		var err error
		if productID == nil {
			p, err = q.CreateProduct(ctx, db.CreateProductParams{
				BrandID: brandID, Name: in.Name, Description: in.Description, BasePrice: in.Price, Active: true, SortOrder: 0,
			})
		} else {
			cur, err2 := q.GetProduct(ctx, *productID)
			if err2 != nil {
				return err2
			}
			if cur.BrandID != brandID {
				return E(403, "forbidden", "Bukan produk kamu.")
			}
			active := in.Active
			p, err = q.UpdateProduct(ctx, db.UpdateProductParams{
				ID: cur.ID, Name: in.Name, Description: in.Description, BasePrice: in.Price, Active: active, PhotoUrl: cur.PhotoUrl, SortOrder: cur.SortOrder,
			})
			if err != nil {
				return err
			}
			if err := q.DeleteVariants(ctx, p.ID); err != nil {
				return E(400, "variant_in_use", "Varian ini sudah kepakai di PO, jadi belum bisa diganti.")
			}
		}
		if err != nil {
			return err
		}
		for i, v := range in.Variants {
			if v.Name == "" {
				continue
			}
			if _, err := q.CreateVariant(ctx, db.CreateVariantParams{
				ProductID: p.ID, Name: v.Name, PriceDelta: v.PriceDelta, Active: true, SortOrder: int32(i),
			}); err != nil {
				return err
			}
		}
		return nil
	})
	return p, err
}

func (a *App) SetProductPhoto(ctx context.Context, userID, productID uuid.UUID, url string) (db.Product, error) {
	p, err := a.Q.GetProduct(ctx, productID)
	if err != nil {
		return db.Product{}, err
	}
	if _, _, err := a.OwnedBrand(ctx, userID, p.BrandID); err != nil {
		return db.Product{}, err
	}
	return a.Q.UpdateProduct(ctx, db.UpdateProductParams{
		ID: p.ID, Name: p.Name, Description: p.Description, BasePrice: p.BasePrice, Active: p.Active, PhotoUrl: &url, SortOrder: p.SortOrder,
	})
}

type ChargeIn struct {
	Name    string
	Amount  int64
	Methods []string
	Active  bool
	Sort    int32
}

func (a *App) SaveCharge(ctx context.Context, userID, brandID uuid.UUID, chargeID *uuid.UUID, in ChargeIn) (db.ExtraCharge, error) {
	if _, _, err := a.OwnedBrand(ctx, userID, brandID); err != nil {
		return db.ExtraCharge{}, err
	}
	if len(in.Name) == 0 || len([]rune(in.Name)) > 30 || in.Amount < 0 {
		return db.ExtraCharge{}, E(400, "bad_charge", "Nama biaya maksimal 30 karakter, nominal tidak boleh minus.")
	}
	methods := in.Methods
	if len(methods) == 0 {
		methods = []string{"pickup", "delivery"}
	}
	for _, m := range methods {
		if m != "pickup" && m != "delivery" {
			return db.ExtraCharge{}, E(400, "bad_charge", "Cara terima cuma ambil sendiri atau diantar.")
		}
	}
	if chargeID == nil {
		n, err := a.Q.CountActiveCharges(ctx, brandID)
		if err != nil {
			return db.ExtraCharge{}, err
		}
		if n >= 10 && in.Active {
			return db.ExtraCharge{}, E(400, "charge_limit", "Maksimal 10 biaya tambahan aktif.")
		}
		return a.Q.CreateCharge(ctx, db.CreateChargeParams{
			BrandID: brandID, Name: in.Name, AmountRp: in.Amount, FulfilmentMethods: methods, SortOrder: in.Sort, Active: in.Active,
		})
	}
	cur, err := a.Q.GetCharge(ctx, *chargeID)
	if err != nil {
		return db.ExtraCharge{}, err
	}
	if cur.BrandID != brandID {
		return db.ExtraCharge{}, E(403, "forbidden", "Bukan biaya kamu.")
	}
	return a.Q.UpdateCharge(ctx, db.UpdateChargeParams{
		ID: cur.ID, Name: in.Name, AmountRp: in.Amount, FulfilmentMethods: methods, Active: in.Active, SortOrder: in.Sort,
	})
}

type BatchItemIn struct {
	ProductID uuid.UUID
	VariantID *uuid.UUID
	Price     int64
	Quota     *int32
}

// Slot is one pickup window. Times are absolute; the label is rendered in the store zone.
type Slot struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type BatchIn struct {
	StoreID           uuid.UUID
	Title             string
	OpensAt           time.Time
	ClosesAt          time.Time
	ReadyAt           time.Time
	PickupSlots       []Slot
	DeadlineMins      *int32
	PaymentDeadlineAt *time.Time
	QuotaTotal        *int32
	AllowPickup       bool
	AllowDelivery     bool
	Notes             string
	Publish           bool
	Items             []BatchItemIn
}

func normalizeBatch(in *BatchIn) error {
	in.OpensAt = clock.Snap15(in.OpensAt)
	in.ClosesAt = clock.Snap15(in.ClosesAt)
	in.ReadyAt = clock.Snap15(in.ReadyAt)
	if in.PaymentDeadlineAt != nil {
		t := clock.Snap15(*in.PaymentDeadlineAt)
		in.PaymentDeadlineAt = &t
	}
	if len(in.PickupSlots) == 0 && !in.ReadyAt.IsZero() {
		in.PickupSlots = []Slot{{Start: in.ReadyAt, End: clock.Snap15(in.ReadyAt.Add(3 * time.Hour))}}
	}
	for i := range in.PickupSlots {
		in.PickupSlots[i].Start = clock.Snap15(in.PickupSlots[i].Start)
		in.PickupSlots[i].End = clock.Snap15(in.PickupSlots[i].End)
	}
	if in.Title == "" || !in.ClosesAt.After(in.OpensAt) {
		return E(400, "bad_batch", "Waktu tutup harus setelah waktu buka.")
	}
	if in.ReadyAt.Before(in.ClosesAt) {
		return E(400, "bad_batch", "Jam siap harus sama atau setelah PO tutup.")
	}
	if len(in.PickupSlots) == 0 {
		return E(400, "bad_batch", "Tambah minimal satu slot ambil.")
	}
	for _, s := range in.PickupSlots {
		if !s.End.After(s.Start) {
			return E(400, "bad_batch", "Jam selesai slot harus setelah jam mulai.")
		}
		if s.Start.Before(in.ReadyAt) {
			return E(400, "bad_batch", "Slot ambil tidak boleh sebelum jam siap.")
		}
	}
	if in.PaymentDeadlineAt != nil && !in.PaymentDeadlineAt.After(in.OpensAt) {
		return E(400, "bad_batch", "Batas bayar harus setelah PO dibuka.")
	}
	return nil
}

func marshalSlots(slots []Slot) (json.RawMessage, error) {
	b, err := json.Marshal(slots)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func ParseSlots(raw json.RawMessage) []Slot {
	if len(raw) == 0 {
		return nil
	}
	var slots []Slot
	_ = json.Unmarshal(raw, &slots)
	return slots
}

func (a *App) CreateBatch(ctx context.Context, userID, brandID uuid.UUID, in BatchIn) (db.PoBatch, error) {
	_, acc, err := a.OwnedBrand(ctx, userID, brandID)
	if err != nil {
		return db.PoBatch{}, err
	}
	locked, _, _, err := a.softLocked(ctx, acc.ID)
	if err != nil {
		return db.PoBatch{}, err
	}
	if locked {
		return db.PoBatch{}, E(402, "soft_lock", "Isi saldo untuk membuka")
	}
	if err := normalizeBatch(&in); err != nil {
		return db.PoBatch{}, err
	}
	if !in.AllowPickup && !in.AllowDelivery {
		return db.PoBatch{}, E(400, "bad_batch", "Pilih ambil sendiri, diantar, atau dua-duanya.")
	}
	if len(in.Items) == 0 {
		return db.PoBatch{}, E(400, "bad_batch", "Tambah minimal satu produk.")
	}
	stores, err := a.Q.ListStores(ctx, brandID)
	if err != nil {
		return db.PoBatch{}, err
	}
	var store db.Store
	if in.StoreID == uuid.Nil {
		if len(stores) == 0 {
			return db.PoBatch{}, E(400, "no_store", "Toko belum ada.")
		}
		store = stores[0]
	} else {
		for _, s := range stores {
			if s.ID == in.StoreID {
				store = s
			}
		}
		if store.ID == uuid.Nil {
			return db.PoBatch{}, E(404, "not_found", "Toko tidak ketemu.")
		}
	}
	status := "draft"
	if in.Publish {
		if in.OpensAt.After(time.Now()) {
			status = "scheduled"
		} else {
			status = "open"
		}
	}
	var batch db.PoBatch
	err = a.tx(ctx, func(q *db.Queries) error {
		code, err := uniqueBatchCode(ctx, q)
		if err != nil {
			return err
		}
		rawSlots, err := marshalSlots(in.PickupSlots)
		if err != nil {
			return err
		}
		batch, err = q.CreateBatch(ctx, db.CreateBatchParams{
			StoreID: store.ID, BrandID: brandID, Code: code, Title: in.Title, Status: status,
			OpensAt: in.OpensAt, ClosesAt: in.ClosesAt, ReadyAt: in.ReadyAt,
			PaymentDeadlineMins: in.DeadlineMins, PaymentDeadlineAt: pgtPtr(in.PaymentDeadlineAt),
			PickupSlots: rawSlots, QuotaTotal: in.QuotaTotal,
			AllowPickup: in.AllowPickup, AllowDelivery: in.AllowDelivery, Notes: in.Notes,
		})
		if err != nil {
			return err
		}
		return insertBatchItems(ctx, q, batch.ID, brandID, in.Items)
	})
	return batch, err
}

func (a *App) UpdateBatch(ctx context.Context, userID, batchID uuid.UUID, in BatchIn) (db.PoBatch, error) {
	if err := a.ensureNotMenuAnchor(ctx, batchID); err != nil {
		return db.PoBatch{}, err
	}
	cur, err := a.Q.GetBatch(ctx, batchID)
	if err != nil {
		return db.PoBatch{}, err
	}
	if _, _, err := a.OwnedBrand(ctx, userID, cur.BrandID); err != nil {
		return db.PoBatch{}, err
	}
	n, err := a.Q.CountBatchOrders(ctx, cur.ID)
	if err != nil {
		return db.PoBatch{}, err
	}
	if err := normalizeBatch(&in); err != nil {
		return db.PoBatch{}, err
	}
	var batch db.PoBatch
	err = a.tx(ctx, func(q *db.Queries) error {
		status := cur.Status
		if in.Publish && (status == "draft" || status == "scheduled") {
			if in.OpensAt.After(time.Now()) {
				status = "scheduled"
			} else {
				status = "open"
			}
		}
		var err error
		rawSlots, err := marshalSlots(in.PickupSlots)
		if err != nil {
			return err
		}
		batch, err = q.UpdateBatch(ctx, db.UpdateBatchParams{
			ID: cur.ID, Title: in.Title, OpensAt: in.OpensAt, ClosesAt: in.ClosesAt, ReadyAt: in.ReadyAt,
			PaymentDeadlineMins: in.DeadlineMins, PaymentDeadlineAt: pgtPtr(in.PaymentDeadlineAt),
			PickupSlots: rawSlots, QuotaTotal: in.QuotaTotal,
			AllowPickup: in.AllowPickup, AllowDelivery: in.AllowDelivery, Notes: in.Notes, Status: status,
		})
		if err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		if err := q.DeleteBatchItems(ctx, cur.ID); err != nil {
			return err
		}
		return insertBatchItems(ctx, q, cur.ID, cur.BrandID, in.Items)
	})
	return batch, err
}

func (a *App) SetBatchOpen(ctx context.Context, userID, batchID uuid.UUID, open bool) (db.PoBatch, error) {
	if err := a.ensureNotMenuAnchor(ctx, batchID); err != nil {
		return db.PoBatch{}, err
	}
	cur, err := a.Q.GetBatch(ctx, batchID)
	if err != nil {
		return db.PoBatch{}, err
	}
	_, acc, err := a.OwnedBrand(ctx, userID, cur.BrandID)
	if err != nil {
		return db.PoBatch{}, err
	}
	if open {
		locked, _, _, err := a.softLocked(ctx, acc.ID)
		if err != nil {
			return db.PoBatch{}, err
		}
		if locked {
			return db.PoBatch{}, E(402, "soft_lock", "Isi saldo untuk membuka")
		}
		if !cur.ClosesAt.After(time.Now()) {
			return db.PoBatch{}, E(400, "batch_closed", "Perpanjang dulu waktu tutupnya, baru dibuka lagi.")
		}
		return a.Q.SetBatchStatus(ctx, db.SetBatchStatusParams{ID: cur.ID, Status: "open"})
	}
	return a.Q.SetBatchStatus(ctx, db.SetBatchStatusParams{ID: cur.ID, Status: "closed"})
}

func (a *App) DuplicateBatch(ctx context.Context, userID, batchID uuid.UUID) (db.PoBatch, error) {
	if err := a.ensureNotMenuAnchor(ctx, batchID); err != nil {
		return db.PoBatch{}, err
	}
	cur, err := a.Q.GetBatch(ctx, batchID)
	if err != nil {
		return db.PoBatch{}, err
	}
	if _, _, err := a.OwnedBrand(ctx, userID, cur.BrandID); err != nil {
		return db.PoBatch{}, err
	}
	items, err := a.Q.ListBatchItems(ctx, cur.ID)
	if err != nil {
		return db.PoBatch{}, err
	}
	span := cur.ClosesAt.Sub(cur.OpensAt)
	ready := cur.ReadyAt.Sub(cur.OpensAt)
	now := time.Now()
	var deadline *time.Time
	if cur.PaymentDeadlineAt.Valid {
		t := now.Add(cur.PaymentDeadlineAt.Time.Sub(cur.OpensAt))
		deadline = &t
	}
	var slots []Slot
	for _, s := range ParseSlots(cur.PickupSlots) {
		slots = append(slots, Slot{
			Start: now.Add(s.Start.Sub(cur.OpensAt)),
			End:   now.Add(s.End.Sub(cur.OpensAt)),
		})
	}
	in := BatchIn{
		StoreID: cur.StoreID, Title: cur.Title, OpensAt: now, ClosesAt: now.Add(span),
		ReadyAt: now.Add(ready), PickupSlots: slots,
		DeadlineMins: cur.PaymentDeadlineMins, PaymentDeadlineAt: deadline, QuotaTotal: cur.QuotaTotal,
		AllowPickup: cur.AllowPickup, AllowDelivery: cur.AllowDelivery, Notes: cur.Notes, Publish: false,
	}
	for _, it := range items {
		var vid *uuid.UUID
		if it.VariantID.Valid {
			id := uuid.UUID(it.VariantID.Bytes)
			vid = &id
		}
		in.Items = append(in.Items, BatchItemIn{ProductID: it.ProductID, VariantID: vid, Price: it.Price, Quota: it.Quota})
	}
	return a.CreateBatch(ctx, userID, cur.BrandID, in)
}

func insertBatchItems(ctx context.Context, q *db.Queries, batchID, brandID uuid.UUID, items []BatchItemIn) error {
	for i, it := range items {
		p, err := q.GetProduct(ctx, it.ProductID)
		if err != nil {
			return err
		}
		if p.BrandID != brandID {
			return E(400, "bad_item", "Produk bukan milik brand ini.")
		}
		name := p.Name
		var vname *string
		price := p.BasePrice
		var vid pgtype.UUID
		if it.VariantID != nil {
			vars, err := q.ListVariantsByProduct(ctx, p.ID)
			if err != nil {
				return err
			}
			var found *db.ProductVariant
			for _, v := range vars {
				if v.ID == *it.VariantID {
					found = &v
					break
				}
			}
			if found == nil {
				return E(400, "bad_item", "Varian tidak ketemu.")
			}
			vname = &found.Name
			price = p.BasePrice + found.PriceDelta
			vid = pgu(found.ID)
		}
		if it.Price > 0 {
			price = it.Price
		}
		if price < 0 {
			return E(400, "bad_item", "Harga tidak boleh minus.")
		}
		if _, err := q.InsertBatchItem(ctx, db.InsertBatchItemParams{
			BatchID: batchID, ProductID: p.ID, VariantID: vid, ProductName: name, VariantName: vname,
			Price: price, Quota: it.Quota, SortOrder: int32(i),
		}); err != nil {
			return err
		}
	}
	return nil
}

func uniqueBatchCode(ctx context.Context, q *db.Queries) (string, error) {
	const alpha = "abcdefghjkmnpqrstuvwxyz23456789"
	for i := 0; i < 8; i++ {
		b := make([]byte, 4)
		for j := range b {
			n, _ := cryptoByte(len(alpha))
			b[j] = alpha[n]
		}
		code := "po-" + string(b)
		if _, err := q.GetBatchByCode(ctx, code); errors.Is(err, pgx.ErrNoRows) {
			return code, nil
		}
	}
	return "", fmt.Errorf("batch code")
}
