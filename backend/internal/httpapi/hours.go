package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"pesenin.product.biz.id/api/internal/db"
	"pesenin.product.biz.id/api/internal/hours"
	"pesenin.product.biz.id/api/internal/service"
)

func (s *Server) meBody(ctx context.Context, me service.Me) (map[string]any, error) {
	var stores []db.Store
	for _, list := range me.Stores {
		stores = append(stores, list...)
	}
	statuses, err := s.App.StoreStatuses(ctx, stores, time.Now())
	if err != nil {
		return nil, err
	}
	return meJSON(me, statuses), nil
}

func (s *Server) storeBody(ctx context.Context, st db.Store) map[string]any {
	stt, err := s.App.StoreStatus(ctx, st, time.Now())
	if err != nil {
		return storeJSON(st, hours.Status{})
	}
	return storeJSON(st, stt)
}

func (s *Server) getStoreHours(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "storeId")
	if err != nil {
		writeErr(w, err)
		return
	}
	view, err := s.App.HoursEditor(r.Context(), user(r).ID, id, time.Now())
	if err != nil {
		writeErr(w, err)
		return
	}
	if view.Days == nil {
		view.Days = []hours.Day{}
	}
	writeJSON(w, 200, view)
}

func (s *Server) putStoreHours(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "storeId")
	if err != nil {
		writeErr(w, err)
		return
	}
	var body struct {
		Inherit                   *bool       `json:"inherit"`
		Timezone                  string      `json:"timezone"`
		AcceptPreordersWhenClosed *bool       `json:"acceptPreordersWhenClosed"`
		Days                      []hours.Day `json:"days"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.App.SaveStoreHours(r.Context(), user(r).ID, id, service.HoursWrite{
		Inherit: body.Inherit, Timezone: body.Timezone, AcceptPreorder: body.AcceptPreordersWhenClosed, Days: body.Days,
	}); err != nil {
		writeErr(w, err)
		return
	}
	s.getStoreHours(w, r)
}

func (s *Server) copyStoreHours(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "storeId")
	if err != nil {
		writeErr(w, err)
		return
	}
	var body struct {
		StoreIDs []uuid.UUID `json:"storeIds"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.App.CopyStoreHours(r.Context(), user(r).ID, id, body.StoreIDs); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) postOverride(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "storeId")
	if err != nil {
		writeErr(w, err)
		return
	}
	var body struct {
		Kind   string `json:"kind"`
		Reason string `json:"reason"`
		Until  string `json:"until"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.App.SetOverride(r.Context(), user(r).ID, id, service.OverrideWrite{
		Kind: body.Kind, Reason: body.Reason, Until: body.Until,
	}); err != nil {
		writeErr(w, err)
		return
	}
	s.getStoreHours(w, r)
}

func (s *Server) deleteOverride(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "storeId")
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.App.ClearOverride(r.Context(), user(r).ID, id); err != nil {
		writeErr(w, err)
		return
	}
	s.getStoreHours(w, r)
}

func (s *Server) postStoreSpecial(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "storeId")
	if err != nil {
		writeErr(w, err)
		return
	}
	in, err := readSpecial(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.App.AddStoreSpecial(r.Context(), user(r).ID, id, in); err != nil {
		writeErr(w, err)
		return
	}
	s.getStoreHours(w, r)
}

func (s *Server) getBrandHours(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "brandId")
	if err != nil {
		writeErr(w, err)
		return
	}
	view, err := s.App.BrandHours(r.Context(), user(r).ID, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *Server) putBrandHours(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "brandId")
	if err != nil {
		writeErr(w, err)
		return
	}
	var body struct {
		Days []hours.Day `json:"days"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.App.SaveBrandHours(r.Context(), user(r).ID, id, body.Days); err != nil {
		writeErr(w, err)
		return
	}
	s.getBrandHours(w, r)
}

func (s *Server) postBrandSpecial(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "brandId")
	if err != nil {
		writeErr(w, err)
		return
	}
	in, err := readSpecial(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if _, err := s.App.AddBrandSpecial(r.Context(), user(r).ID, id, in); err != nil {
		writeErr(w, err)
		return
	}
	s.getBrandHours(w, r)
}

func (s *Server) deleteSpecial(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "specialId")
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.App.DeleteSpecial(r.Context(), user(r).ID, id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func readSpecial(r *http.Request) (service.SpecialWrite, error) {
	var body struct {
		DateFrom string       `json:"dateFrom"`
		DateTo   string       `json:"dateTo"`
		Closed   bool         `json:"closed"`
		Slots    []hours.Slot `json:"slots"`
		Note     string       `json:"note"`
	}
	if err := readJSON(r, &body); err != nil {
		return service.SpecialWrite{}, err
	}
	return service.SpecialWrite{DateFrom: body.DateFrom, DateTo: body.DateTo, Closed: body.Closed, Slots: body.Slots, Note: body.Note}, nil
}

func (s *Server) adminStores(w http.ResponseWriter, r *http.Request) {
	if err := s.mustAdmin(w, r); err != nil {
		return
	}
	rows, err := s.App.AdminStores(r.Context(), time.Now())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{
		"stores":    rows,
		"serverNow": time.Now().UTC().Format(time.RFC3339),
	})
}
