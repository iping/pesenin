package httpapi

import "net/http"

func (s *Server) adminKYCBrands(w http.ResponseWriter, r *http.Request) {
	if err := s.mustAdmin(w, r); err != nil {
		return
	}
	rows, err := s.App.ListKYCBrands(r.Context(), r.URL.Query().Get("status"))
	if err != nil {
		writeErr(w, err)
		return
	}
	out := []any{}
	for _, row := range rows {
		out = append(out, map[string]any{
			"id": row.ID, "name": row.Name, "slug": row.Slug,
			"status": row.Status, "reason": row.Reason, "updatedAt": ts(row.UpdatedAt),
			"ownerEmail": row.OwnerEmail, "ownerWhatsapp": row.OwnerWhatsapp,
			"emailVerified": row.EmailVerified, "whatsappVerified": row.WAVerified,
		})
	}
	writeJSON(w, 200, out)
}

func (s *Server) adminKYCBrand(w http.ResponseWriter, r *http.Request) {
	if err := s.mustAdmin(w, r); err != nil {
		return
	}
	id, err := pathUUID(r, "brandId")
	if err != nil {
		writeErr(w, err)
		return
	}
	events, err := s.App.KYCEvents(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	status, reason, err := s.App.BrandKYC(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	hist := []any{}
	for _, ev := range events {
		hist = append(hist, map[string]any{
			"from": ev.From, "to": ev.To, "reason": ev.Reason,
			"actorId": ev.ActorID, "createdAt": ts(ev.CreatedAt),
		})
	}
	writeJSON(w, 200, map[string]any{"id": id, "status": status, "reason": reason, "events": hist})
}

func (s *Server) adminKYCApprove(w http.ResponseWriter, r *http.Request) {
	s.adminKYCDecision(w, r, "approve")
}

func (s *Server) adminKYCReject(w http.ResponseWriter, r *http.Request) {
	s.adminKYCDecision(w, r, "reject")
}

func (s *Server) adminKYCSuspend(w http.ResponseWriter, r *http.Request) {
	s.adminKYCDecision(w, r, "suspend")
}

func (s *Server) adminKYCDecision(w http.ResponseWriter, r *http.Request, decision string) {
	if err := s.mustAdmin(w, r); err != nil {
		return
	}
	id, err := pathUUID(r, "brandId")
	if err != nil {
		writeErr(w, err)
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.App.ReviewBrand(r.Context(), user(r), id, decision, body.Reason); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
