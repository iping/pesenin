package httpapi

import (
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"pesenin.product.biz.id/api/internal/clock"
	"pesenin.product.biz.id/api/internal/db"
	"pesenin.product.biz.id/api/internal/hours"
	"pesenin.product.biz.id/api/internal/images"
	"pesenin.product.biz.id/api/internal/service"
)

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func tsP(t pgtype.Timestamptz) any {
	if !t.Valid {
		return nil
	}
	return ts(t.Time)
}

func meJSON(me service.Me, byStore map[uuid.UUID]hours.Status) map[string]any {
	kycBy := map[uuid.UUID]service.BrandKYCView{}
	for _, k := range me.KYC {
		kycBy[k.ID] = k
	}
	out := map[string]any{
		"user": map[string]any{
			"id": me.User.ID, "email": me.User.Email, "name": me.User.Name,
			"avatarUrl": me.User.AvatarUrl, "isPlatformAdmin": me.User.IsPlatformAdmin,
		},
		"verification": verificationJSON(me),
		"softLocked":   me.Locked, "lowBalance": me.Low, "graceFloor": me.Floor, "orderFee": me.Fee,
	}
	if me.Account != nil {
		out["account"] = map[string]any{"id": me.Account.ID, "name": me.Account.Name, "status": me.Account.Status}
	} else {
		out["account"] = nil
	}
	if me.Wallet != nil {
		eq := me.Wallet.BalanceKoin / 100
		if me.Fee > 0 {
			eq = me.Wallet.BalanceKoin / me.Fee
		}
		out["wallet"] = map[string]any{
			"balanceKoin": me.Wallet.BalanceKoin, "orderEquivalent": eq,
		}
	} else {
		out["wallet"] = nil
	}
	brands := []any{}
	for _, b := range me.Brands {
		stores := []any{}
		for _, st := range me.Stores[b.ID] {
			stores = append(stores, storeJSON(st, byStore[st.ID]))
		}
		brands = append(brands, map[string]any{
			"id": b.ID, "name": b.Name, "slug": b.Slug, "description": b.Description,
			"waNumber": b.WaNumber, "logoUrl": b.LogoUrl, "stores": stores,
			"kycStatus": kycBy[b.ID].Status, "kycReason": kycBy[b.ID].Reason,
		})
	}
	out["brands"] = brands
	return out
}

func verificationJSON(me service.Me) map[string]any {
	brands := []any{}
	for _, k := range me.KYC {
		brands = append(brands, map[string]any{
			"id": k.ID, "name": k.Name, "slug": k.Slug,
			"status": k.Status, "reason": k.Reason,
		})
	}
	if brands == nil {
		brands = []any{}
	}
	return map[string]any{
		"emailVerified":    me.Contact.EmailVerified,
		"whatsapp":         me.Contact.Whatsapp,
		"whatsappVerified": me.Contact.WhatsappVerified,
		"hasPassword":      me.Contact.HasPassword,
		"kycGate":          me.Contact.Gate,
		"brands":           brands,
	}
}

func storeJSON(st db.Store, status hours.Status) map[string]any {
	body := map[string]any{
		"id": st.ID, "name": st.Name, "slug": st.Slug, "address": st.Address, "mapsUrl": st.MapsUrl,
		"timezone": st.Timezone,
		"isOpen":   st.IsOpen, "acceptPreordersWhenClosed": st.AcceptPreordersWhenClosed, "isDefault": st.IsDefault,
	}
	if status.Timezone != "" || status.Source != "" {
		body["hours"] = status
	}
	return body
}

func batchJSON(b db.PoBatch, items []db.BatchItem, tz string) map[string]any {
	its := []any{}
	for _, it := range items {
		left := any(nil)
		if it.Quota != nil {
			rem := *it.Quota - it.SoldCount
			if rem < 0 {
				rem = 0
			}
			left = rem
		}
		its = append(its, map[string]any{
			"id": it.ID, "productId": it.ProductID, "variantId": uuidOrNil(it.VariantID),
			"name": it.ProductName, "variantName": it.VariantName, "price": it.Price,
			"quota": it.Quota, "sold": it.SoldCount, "remaining": left,
		})
	}
	if tz == "" {
		tz = "Asia/Jakarta"
	}
	return map[string]any{
		"id": b.ID, "code": b.Code, "title": b.Title, "status": b.Status,
		"opensAt": ts(b.OpensAt), "closesAt": ts(b.ClosesAt), "readyAt": ts(b.ReadyAt),
		"pickupSlots":       slotJSON(b.PickupSlots, tz),
		"paymentDeadlineAt": tsP(b.PaymentDeadlineAt), "paymentDeadlineMins": b.PaymentDeadlineMins, "quotaTotal": b.QuotaTotal,
		"allowPickup": b.AllowPickup, "allowDelivery": b.AllowDelivery, "notes": b.Notes,
		"timezone": tz, "serverNow": time.Now().UTC().Format(time.RFC3339),
		"storeId": b.StoreID, "brandId": b.BrandID, "items": its,
	}
}

func slotJSON(raw []byte, tz string) []any {
	slots := service.ParseSlots(raw)
	out := []any{}
	for _, s := range slots {
		out = append(out, map[string]any{
			"start": ts(s.Start), "end": ts(s.End), "label": clock.Slot(s.Start, s.End, tz),
		})
	}
	return out
}

func orderJSON(d service.OrderDetail) map[string]any {
	items := []any{}
	for _, it := range d.Items {
		items = append(items, map[string]any{
			"name": it.ProductName, "variantName": it.VariantName, "unitPrice": it.UnitPrice, "qty": it.Qty,
		})
	}
	charges := []any{}
	for _, c := range d.Charges {
		charges = append(charges, map[string]any{"name": c.Name, "amount": c.ComputedRp})
	}
	var reject any
	for _, p := range d.Proofs {
		if p.ReviewStatus == "rejected" && p.RejectReason != nil {
			reject = *p.RejectReason
		}
	}
	proofs := []any{}
	for _, p := range d.Proofs {
		v := images.Describe(p.ObjectKey)
		proofs = append(proofs, map[string]any{
			"id": p.ID, "url": v.Src, "displayUrl": v.Display, "thumbUrl": v.Thumb,
			"srcset": v.Srcset, "width": v.FullWidth, "mime": p.Mime,
			"status": p.ReviewStatus, "rejectReason": p.RejectReason, "uploadedAt": ts(p.UploadedAt),
		})
	}
	return map[string]any{
		"id": d.Order.ID, "code": d.Order.Code, "publicToken": d.Order.PublicToken,
		"statusUrl":     "/o/" + d.Order.PublicToken,
		"paymentStatus": d.Order.PaymentStatus, "fulfilmentStatus": d.Order.FulfilmentStatus,
		"fulfilmentMethod": d.Order.FulfilmentMethod, "address": d.Order.Address, "note": d.Order.Note,
		"subtotal": d.Order.Subtotal, "extraChargesTotal": d.Order.ExtraChargesTotal,
		"baseTotal": d.Order.BaseTotal, "uniqueSuffix": d.Order.UniqueSuffix, "totalDue": d.Order.TotalDue,
		"expiresAt": tsP(d.Order.ExpiresAt), "paidAt": tsP(d.Order.PaidAt), "createdAt": ts(d.Order.CreatedAt),
		"pickupSlotStart": tsP(d.Order.PickupSlotStart), "pickupSlotEnd": tsP(d.Order.PickupSlotEnd),
		"serverNow": time.Now().UTC().Format(time.RFC3339),
		"items":     items, "charges": charges, "latestRejectReason": reject, "proofs": proofs,
		"customer": map[string]any{"id": d.Customer.ID, "name": d.Customer.Name, "wa": d.Customer.WaNumberE164, "ordersCount": d.Customer.OrdersCount},
		"bank":     map[string]any{"bankCode": d.Bank.BankCode, "accountNumber": d.Bank.AccountNumber, "holderName": d.Bank.HolderName},
		"batch": map[string]any{
			"id": d.Batch.ID, "code": d.Batch.Code, "title": d.Batch.Title,
			"readyAt": ts(d.Batch.ReadyAt), "closesAt": ts(d.Batch.ClosesAt),
			"pickupSlots": slotJSON(d.Batch.PickupSlots, d.Store.Timezone),
		},
		"pickupSlotLabel": pickupLabel(d),
		"store":           map[string]any{"name": d.Store.Name, "address": d.Store.Address, "isOpen": d.Store.IsOpen, "timezone": d.Store.Timezone},
		"brand":           map[string]any{"name": d.Brand.Name, "slug": d.Brand.Slug, "waNumber": d.Brand.WaNumber},
	}
}

func pickupLabel(d service.OrderDetail) any {
	if !d.Order.PickupSlotStart.Valid || !d.Order.PickupSlotEnd.Valid {
		return nil
	}
	tz := d.Store.Timezone
	if tz == "" {
		tz = "Asia/Jakarta"
	}
	return clock.Slot(d.Order.PickupSlotStart.Time, d.Order.PickupSlotEnd.Time, tz)
}

func applyPhoto(m map[string]any, url *string) {
	if url == nil || *url == "" {
		m["photoUrl"] = nil
		return
	}
	v := images.Describe(*url)
	m["photoUrl"] = v.Src
	if v.Srcset != "" {
		m["photoSrcset"] = v.Srcset
		m["photoThumbUrl"] = v.Thumb
	}
}

func uuidOrNil(id pgtype.UUID) any {
	if !id.Valid {
		return nil
	}
	return uuid.UUID(id.Bytes)
}

func topupJSON(t db.Topup, bank *db.PlatformBankAccount) map[string]any {
	out := map[string]any{
		"id": t.ID, "code": t.Code, "amountRp": t.AmountRp, "uniqueSuffix": t.UniqueSuffix,
		"adminFeeRp": t.AdminFeeRp, "totalDueRp": t.TotalDueRp, "koinToCredit": t.KoinToCredit,
		"status": t.Status, "expiresAt": ts(t.ExpiresAt), "createdAt": ts(t.CreatedAt),
		"rejectReason": t.RejectReason, "receivedAmountRp": t.ReceivedAmountRp,
		"proofUrl": nil, "serverNow": time.Now().UTC().Format(time.RFC3339),
	}
	if t.ProofKey != nil {
		v := images.Describe(*t.ProofKey)
		out["proofUrl"] = v.Src
		out["proofDisplayUrl"] = v.Display
		out["proofSrcset"] = v.Srcset
		out["proofWidth"] = v.FullWidth
	}
	if bank != nil {
		out["bank"] = map[string]any{"bankCode": bank.BankCode, "accountNumber": bank.AccountNumber, "holderName": bank.HolderName}
	}
	return out
}
