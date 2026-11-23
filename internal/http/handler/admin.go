package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/dombyte/pvmon/internal/auth"
)

// defaultAuditDays is the range of GET /api/v1/audit without parameters.
const defaultAuditDays = 30

const hoursPerDay = 24

// usersBody wraps the user list.
type usersBody struct {
	Users []auth.UserInfo `json:"users"`
}

// Users serves GET /api/v1/users.
func (h *Auth) Users(w http.ResponseWriter, r *http.Request) {
	us, err := h.svc.Users(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, usersBody{Users: us}, h.log)
}

// addUserRequest is the body of POST /api/v1/users.
type addUserRequest struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	Lang     string `json:"lang"`
}

// AddUser serves POST /api/v1/users.
func (h *Auth) AddUser(w http.ResponseWriter, r *http.Request) {
	var req addUserRequest
	if !h.decode(w, r, &req) {
		return
	}
	u, err := h.svc.AddUser(r.Context(), h.actor(r), req.Name, req.Password, req.Lang)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, userBody{User: u}, h.log)
}

// DeleteUser serves DELETE /api/v1/users/{id}.
func (h *Auth) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := h.userID(w, r)
	if !ok {
		return
	}
	if err := h.svc.DeleteUser(r.Context(), h.actor(r), id); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// passwordRequest is the body of PUT /api/v1/users/{id}/password.
type passwordRequest struct {
	Password string `json:"password"`
}

// SetPassword serves PUT /api/v1/users/{id}/password; the user's sessions end.
func (h *Auth) SetPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := h.userID(w, r)
	if !ok {
		return
	}
	var req passwordRequest
	if !h.decode(w, r, &req) {
		return
	}
	if err := h.svc.SetPassword(r.Context(), h.actor(r), id, req.Password); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Auth) userID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		h.fail(w, apiError{
			http.StatusBadRequest, "invalid_param", "invalid user id",
			[]errorDetail{{"id", "want an integer"}},
		})
		return 0, false
	}
	return id, true
}

// actor is the admin principal; the router only reaches these handlers with one.
func (h *Auth) actor(r *http.Request) auth.Principal {
	p, _ := auth.PrincipalFrom(r.Context()) // guarded by middleware.Admin
	return p
}

// tokensBody wraps the token list.
type tokensBody struct {
	Tokens []auth.TokenInfo `json:"tokens"`
}

// Tokens serves GET /api/v1/tokens.
func (h *Auth) Tokens(w http.ResponseWriter, r *http.Request) {
	ts, err := h.svc.Tokens(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tokensBody{Tokens: ts}, h.log)
}

// tokenRequest is the body of POST /api/v1/tokens; ExpiresDays 0 = never.
type tokenRequest struct {
	Name        string `json:"name"`
	ExpiresDays int    `json:"expires_days"`
}

// CreateToken serves POST /api/v1/tokens; the secret is returned only here.
func (h *Auth) CreateToken(w http.ResponseWriter, r *http.Request) {
	var req tokenRequest
	if !h.decode(w, r, &req) {
		return
	}
	exp := time.Duration(req.ExpiresDays) * hoursPerDay * time.Hour
	t, err := h.svc.CreateToken(r.Context(), h.actor(r), req.Name, exp)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, t, h.log)
}

// RevokeToken serves DELETE /api/v1/tokens/{id}.
func (h *Auth) RevokeToken(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.RevokeToken(r.Context(), h.actor(r), r.PathValue("id")); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// auditBody wraps the audit entries.
type auditBody struct {
	Entries []auth.AuditInfo `json:"entries"`
}

// Audit serves GET /api/v1/audit?from=&to= (local dates or RFC 3339, to inclusive as a
// date); default the last 30 days.
func (h *Auth) Audit(w http.ResponseWriter, r *http.Request) {
	from, to, details := h.auditRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if len(details) > 0 {
		h.fail(w, apiError{http.StatusBadRequest, "invalid_param", "invalid range", details})
		return
	}
	es, err := h.svc.Audit(r.Context(), from, to)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, auditBody{Entries: es}, h.log)
}

func (h *Auth) auditRange(fromS, toS string) (from, to time.Time, d []errorDetail) {
	to = h.clock.Now().In(h.loc)
	from = to.AddDate(0, 0, -defaultAuditDays)
	var err error
	if fromS != "" {
		if from, err = parseLocalDate(fromS, h.loc); err != nil {
			d = append(d, errorDetail{"from", err.Error()})
		}
	}
	if toS != "" {
		if to, err = parseLocalDate(toS, h.loc); err != nil {
			d = append(d, errorDetail{"to", err.Error()})
		}
		to = to.AddDate(0, 0, 1)
	}
	return from, to, d
}
