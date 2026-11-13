package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"pesenin.product.biz.id/api/internal/db"
	"pesenin.product.biz.id/api/internal/service"
)

func (s *Server) presentBatch(ctx context.Context, b db.PoBatch, items []db.BatchItem, tz string) map[string]any {
	view := batchJSON(b, items, tz)
	s.attachCapacity(ctx, view)
	return view
}

func (s *Server) attachCapacity(ctx context.Context, view map[string]any) {
	id, ok := view["id"].(uuid.UUID)
	if !ok {
		return
	}
	cap, err := s.App.BatchCapacityView(ctx, id)
	if err != nil {
		return
	}
	view["capacity"] = cap
	items, _ := view["items"].([]any)
	for i, raw := range items {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		itemID, ok := m["id"].(uuid.UUID)
		if !ok {
			continue
		}
		if lim, ok := cap.Items[itemID]; ok {
			m["minQty"] = lim.MinQty
			m["maxPerCustomer"] = lim.MaxPerCustomer
			if lim.Remaining != nil {
				m["remaining"] = *lim.Remaining
			}
		}
		items[i] = m
	}
	slots, _ := view["pickupSlots"].([]any)
	for i, raw := range slots {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		start, _ := m["start"].(string)
		for _, sl := range cap.Slots {
			if ts(sl.Start) == start {
				m["capacityQty"] = sl.CapacityQty
				m["remainingQty"] = sl.RemainingQty
				m["capacityOrders"] = sl.CapacityOrders
				m["remainingOrders"] = sl.RemainingOrders
				break
			}
		}
		slots[i] = m
	}
}

func (s *Server) getCapacity(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "brandId")
	if err != nil {
		writeErr(w, err)
		return
	}
	out, err := s.App.CapacitySettings(r.Context(), user(r).ID, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) putBrandCapacity(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "brandId")
	if err != nil {
		writeErr(w, err)
		return
	}
	var in service.CapacityInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.App.SaveBrandCapacity(r.Context(), user(r).ID, id, in); err != nil {
		writeErr(w, err)
		return
	}
	out, err := s.App.CapacitySettings(r.Context(), user(r).ID, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) putStoreCapacity(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "storeId")
	if err != nil {
		writeErr(w, err)
		return
	}
	var body struct {
		service.CapacityInput
		Inherit bool `json:"inherit"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.App.SaveStoreCapacity(r.Context(), user(r).ID, id, body.CapacityInput, body.Inherit); err != nil {
		writeErr(w, err)
		return
	}
	st, err := s.App.Q.GetStore(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	out, err := s.App.CapacitySettings(r.Context(), user(r).ID, st.BrandID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) putProductCapacity(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "productId")
	if err != nil {
		writeErr(w, err)
		return
	}
	var in service.ProductCapacityInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.App.SaveProductCapacity(r.Context(), user(r).ID, id, in); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) putBatchCapacity(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "batchId")
	if err != nil {
		writeErr(w, err)
		return
	}
	var in service.BatchCapacityInput
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.App.AdjustBatchCapacity(r.Context(), user(r).ID, id, in); err != nil {
		writeErr(w, err)
		return
	}
	b, err := s.App.Q.GetBatch(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	items, _ := s.App.Q.ListBatchItems(r.Context(), b.ID)
	writeJSON(w, 200, s.presentBatch(r.Context(), b, items, ""))
}

func (s *Server) quotaDecision(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "batchId")
	if err != nil {
		writeErr(w, err)
		return
	}
	var body struct {
		Decision string `json:"decision"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	proceed := body.Decision == "proceed"
	if body.Decision != "proceed" && body.Decision != "cancel" {
		writeErr(w, service.E(400, "bad_decision", "Pilih lanjutkan atau batalkan."))
		return
	}
	if err := s.App.DecideMinimum(r.Context(), user(r).ID, id, proceed); err != nil {
		writeErr(w, err)
		return
	}
	b, err := s.App.Q.GetBatch(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	items, _ := s.App.Q.ListBatchItems(r.Context(), b.ID)
	writeJSON(w, 200, s.presentBatch(r.Context(), b, items, ""))
}

func (s *Server) joinWaitlist(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string     `json:"name"`
		WhatsApp    string     `json:"whatsapp"`
		Qty         int32      `json:"qty"`
		BatchItemID *uuid.UUID `json:"batchItemId"`
		SlotIndex   int        `json:"slotIndex"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	out, err := s.App.JoinWaitlist(r.Context(), chi.URLParam(r, "code"), service.WaitlistIn{
		Name: body.Name, WhatsApp: body.WhatsApp, Qty: body.Qty, BatchItemID: body.BatchItemID, SlotIndex: body.SlotIndex,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, out)
}

func (s *Server) waitlistStatus(w http.ResponseWriter, r *http.Request) {
	out, err := s.App.WaitlistStatus(r.Context(), chi.URLParam(r, "token"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) adminCapacity(w http.ResponseWriter, r *http.Request) {
	if err := s.mustAdmin(w, r); err != nil {
		return
	}
	rows, err := s.App.CapacityStats(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"stores": rows, "serverNow": time.Now().UTC().Format(time.RFC3339)})
}
