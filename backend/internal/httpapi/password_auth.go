package httpapi

import (
	"net"
	"net/http"

	"pesenin.product.biz.id/api/internal/db"
)

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || host == "" {
		if r.RemoteAddr == "" {
			return "unknown"
		}
		return r.RemoteAddr
	}
	return host
}

func (s *Server) passwordSignup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	msg, err := s.App.SignUp(r.Context(), body.Email, body.Password, body.Name, clientIP(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": msg})
}

func (s *Server) passwordLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	u, err := s.App.LoginPassword(r.Context(), body.Email, body.Password, clientIP(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	s.sessionJSON(w, r, u, "password")
}

func (s *Server) passwordForgot(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	msg, err := s.App.RequestPasswordReset(r.Context(), body.Email, clientIP(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": msg})
}

func (s *Server) passwordReset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	u, err := s.App.ResetPassword(r.Context(), body.Token, body.Password)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.sessionJSON(w, r, u, "password")
}

func (s *Server) emailVerify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	u, err := s.App.VerifyEmail(r.Context(), body.Token)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.sessionJSON(w, r, u, "password")
}

func (s *Server) emailResend(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	msg, err := s.App.ResendVerification(r.Context(), body.Email, clientIP(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": msg})
}

func (s *Server) passwordLink(w http.ResponseWriter, r *http.Request) {
	msg, err := s.App.RequestPasswordLink(r.Context(), user(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": msg})
}

func (s *Server) emailChange(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	u, err := s.App.ChangeEmail(r.Context(), user(r), body.Email, body.Password)
	if err != nil {
		writeErr(w, err)
		return
	}
	me, err := s.App.LoadMe(r.Context(), u)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, meJSON(me))
}

func (s *Server) whatsappOTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Phone string `json:"phone"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.App.RequestWhatsAppOTP(r.Context(), user(r), body.Phone, clientIP(r)); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "message": "Kode dikirim ke WhatsApp kamu. Berlaku 5 menit."})
}

func (s *Server) whatsappCheck(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.App.CheckWhatsAppOTP(r.Context(), user(r), body.Code); err != nil {
		writeErr(w, err)
		return
	}
	me, err := s.App.LoadMe(r.Context(), user(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, meJSON(me))
}

func (s *Server) verification(w http.ResponseWriter, r *http.Request) {
	me, err := s.App.LoadMe(r.Context(), user(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, verificationJSON(me))
}

func (s *Server) kycSubmit(w http.ResponseWriter, r *http.Request) {
	id, err := pathUUID(r, "brandId")
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.App.SubmitBrandKYC(r.Context(), user(r), id); err != nil {
		writeErr(w, err)
		return
	}
	me, err := s.App.LoadMe(r.Context(), user(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, meJSON(me))
}

func (s *Server) sessionJSON(w http.ResponseWriter, r *http.Request, u db.User, via string) {
	raw, err := s.App.StartSessionVia(r.Context(), u.ID, r.UserAgent(), r.RemoteAddr, via)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.setSession(w, raw)
	me, err := s.App.LoadMe(r.Context(), u)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, meJSON(me))
}
