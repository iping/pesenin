package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"pesenin.product.biz.id/api/internal/db"
	"pesenin.product.biz.id/api/internal/service"
)

func nutritionJSON(n db.NutritionRow) any {
	if n.ProductID == uuid.Nil {
		return nil
	}
	tags := n.Tags
	if tags == nil {
		tags = []string{}
	}
	allergens := n.Allergens
	if allergens == nil {
		allergens = []string{}
	}
	return map[string]any{
		"kcal": n.Kcal, "proteinG": n.ProteinG, "carbG": n.CarbG, "fatG": n.FatG,
		"allergens": allergens, "tags": tags,
	}
}

func (s *Server) publicMenu(w http.ResponseWriter, r *http.Request) {
	view, err := s.App.PublicMenuBySlug(r.Context(), chi.URLParam(r, "slug"), r.URL.Query().Get("store"), r.URL.Query().Get("start"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *Server) quoteMenu(w http.ResponseWriter, r *http.Request) {
	storeID, err := pathUUID(r, "storeId")
	if err != nil {
		writeErr(w, err)
		return
	}
	var body service.MenuOrderIn
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	q, err := s.App.QuoteMenu(r.Context(), storeID, body)
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

func (s *Server) placeMenu(w http.ResponseWriter, r *http.Request) {
	storeID, err := pathUUID(r, "storeId")
	if err != nil {
		writeErr(w, err)
		return
	}
	var body service.MenuOrderIn
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	if body.IdempotencyKey == "" {
		body.IdempotencyKey = r.Header.Get("Idempotency-Key")
	}
	o, err := s.App.PlaceMenuOrder(r.Context(), storeID, body)
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

func (s *Server) listMenuTemplates(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "brandId")
	if err != nil {
		writeErr(w, err)
		return
	}
	rows, err := s.App.ListMenuTemplates(r.Context(), user(r).ID, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, rows)
}

func (s *Server) saveMenuTemplate(w http.ResponseWriter, r *http.Request) {
	brandID, err := pathUUID(r, "brandId")
	if err != nil {
		writeErr(w, err)
		return
	}
	var id *uuid.UUID
	if raw := chi.URLParam(r, "templateId"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			writeErr(w, service.E(400, "bad_id", "ID template tidak valid."))
			return
		}
		id = &parsed
	}
	var body service.TemplateIn
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	view, err := s.App.SaveMenuTemplate(r.Context(), user(r).ID, brandID, id, body)
	if err != nil {
		writeErr(w, err)
		return
	}
	status := 200
	if id == nil {
		status = 201
	}
	writeJSON(w, status, view)
}

func (s *Server) sellerMenuWeek(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "storeId")
	if err != nil {
		writeErr(w, err)
		return
	}
	view, err := s.App.MenuWeek(r.Context(), user(r).ID, id, r.URL.Query().Get("start"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *Server) getMenuSettings(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "storeId")
	if err != nil {
		writeErr(w, err)
		return
	}
	view, err := s.App.GetStoreMenuSettings(r.Context(), user(r).ID, id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *Server) putMenuSettings(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "storeId")
	if err != nil {
		writeErr(w, err)
		return
	}
	var body service.MenuSettingsIn
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	view, err := s.App.SaveStoreMenuSettings(r.Context(), user(r).ID, id, body)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *Server) putMenuDay(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "storeId")
	if err != nil {
		writeErr(w, err)
		return
	}
	var body service.MenuDayIn
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	view, err := s.App.SaveMenuDay(r.Context(), user(r).ID, id, chi.URLParam(r, "date"), body)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *Server) deleteMenuDay(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "storeId")
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.App.ClearMenuDay(r.Context(), user(r).ID, id, chi.URLParam(r, "date")); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) copyMenuWeek(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "storeId")
	if err != nil {
		writeErr(w, err)
		return
	}
	var body struct {
		From string `json:"from"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	view, err := s.App.CopyMenuWeek(r.Context(), user(r).ID, id, body.From)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *Server) menuPrep(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "storeId")
	if err != nil {
		writeErr(w, err)
		return
	}
	view, err := s.App.MenuPrep(r.Context(), user(r).ID, id, r.URL.Query().Get("date"), r.URL.Query().Get("includeUnpaid") == "1")
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *Server) menuRuns(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "storeId")
	if err != nil {
		writeErr(w, err)
		return
	}
	view, err := s.App.MenuRuns(r.Context(), user(r).ID, id, r.URL.Query().Get("date"), r.URL.Query().Get("includeUnpaid") == "1")
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, view)
}

func (s *Server) menuPrepCSV(w http.ResponseWriter, r *http.Request) {
	s.menuCSV(w, r, true)
}

func (s *Server) menuRunsCSV(w http.ResponseWriter, r *http.Request) {
	s.menuCSV(w, r, false)
}

func (s *Server) menuCSV(w http.ResponseWriter, r *http.Request, prep bool) {
	id, err := pathUUID(r, "storeId")
	if err != nil {
		writeErr(w, err)
		return
	}
	var csv string
	if prep {
		csv, err = s.App.MenuPrepCSV(r.Context(), user(r).ID, id, r.URL.Query().Get("date"), r.URL.Query().Get("includeUnpaid") == "1")
	} else {
		csv, err = s.App.MenuRunsCSV(r.Context(), user(r).ID, id, r.URL.Query().Get("date"), r.URL.Query().Get("includeUnpaid") == "1")
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	name := "antar-ambil.csv"
	if prep {
		name = "daftar-masak.csv"
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+name)
	_, _ = w.Write([]byte(csv))
}

func (s *Server) putNutrition(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "productId")
	if err != nil {
		writeErr(w, err)
		return
	}
	var body service.NutritionIn
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	view, err := s.App.SaveNutrition(r.Context(), user(r).ID, id, body)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, view)
}
