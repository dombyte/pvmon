// Package router declares every HTTP route and its access level in one place.
package router

import "net/http"

// Handlers are the handlers of all routes.
type Handlers struct {
	Health  http.Handler
	Version http.Handler
	Site    http.Handler
	Energy  http.Handler
	Gauge   http.Handler
	Report  http.Handler
	Devices DeviceHandlers
	Auth    AuthHandlers
	Config  ConfigHandlers
	WS      http.Handler
	UI      http.Handler
}

// DeviceHandlers serve the device endpoints.
type DeviceHandlers struct {
	List, Get, Status, Overview http.HandlerFunc
}

// AuthHandlers serve sessions, setup, users, tokens and the audit log.
type AuthHandlers struct {
	Login, Logout, Me, Setup                   http.HandlerFunc
	Users, AddUser, DeleteUser, SetPassword    http.HandlerFunc
	Tokens, CreateToken, RevokeToken, AuditLog http.HandlerFunc
}

// ConfigHandlers serve the UI configuration and the profile list.
type ConfigHandlers struct {
	Overview, Settings, PutSettings, Export   http.HandlerFunc
	Object, PutObject, DeleteObject, Profiles http.HandlerFunc
	PutObjects, Probe                         http.HandlerFunc
}

// Guards wrap a route with its access level (spec 9.1): Read for "read" endpoints
// (open when auth.public_read is set), Admin for "admin" endpoints.
type Guards struct {
	Read, Admin func(http.Handler) http.Handler
}

// New returns the mux with all routes.
func New(h Handlers, g Guards) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /health", h.Health)
	mux.Handle("GET /api/v1/version", h.Version)
	read(mux, g, h)
	open(mux, h.Auth)
	admin(mux, g, h.Auth)
	config(mux, g, h.Config)
	mux.Handle("GET /", h.UI)
	return mux
}

func read(mux *http.ServeMux, g Guards, h Handlers) {
	routes := map[string]http.Handler{
		"GET /api/v1/site":                h.Site,
		"GET /api/v1/energy":              h.Energy,
		"GET /api/v1/gauge":               h.Gauge,
		"GET /api/v1/report":              h.Report,
		"GET /api/v1/devices":             h.Devices.List,
		"GET /api/v1/devices/{id}":        h.Devices.Get,
		"GET /api/v1/devices/{id}/status": h.Devices.Status,
		"GET /api/v1/status":              h.Devices.Overview,
		"GET /ws":                         h.WS,
	}
	for p, hd := range routes {
		mux.Handle(p, g.Read(hd))
	}
}

// open routes need no login; logout checks its session itself.
func open(mux *http.ServeMux, h AuthHandlers) {
	mux.Handle("POST /api/v1/auth/login", h.Login)
	mux.Handle("POST /api/v1/auth/logout", h.Logout)
	mux.Handle("GET /api/v1/auth/me", h.Me)
	mux.Handle("POST /api/v1/setup", h.Setup)
}

func admin(mux *http.ServeMux, g Guards, h AuthHandlers) {
	routes := map[string]http.Handler{
		"GET /api/v1/users":               h.Users,
		"POST /api/v1/users":              h.AddUser,
		"DELETE /api/v1/users/{id}":       h.DeleteUser,
		"PUT /api/v1/users/{id}/password": h.SetPassword,
		"GET /api/v1/tokens":              h.Tokens,
		"POST /api/v1/tokens":             h.CreateToken,
		"DELETE /api/v1/tokens/{id}":      h.RevokeToken,
		"GET /api/v1/audit":               h.AuditLog,
	}
	for p, hd := range routes {
		mux.Handle(p, g.Admin(hd))
	}
}

func config(mux *http.ServeMux, g Guards, h ConfigHandlers) {
	routes := map[string]http.Handler{
		"GET /api/v1/config":                h.Overview,
		"PUT /api/v1/config":                h.PutObjects,
		"POST /api/v1/config/probe":         h.Probe,
		"GET /api/v1/config/settings":       h.Settings,
		"PUT /api/v1/config/settings":       h.PutSettings,
		"GET /api/v1/config/export":         h.Export,
		"GET /api/v1/config/{kind}/{id}":    h.Object,
		"PUT /api/v1/config/{kind}/{id}":    h.PutObject,
		"DELETE /api/v1/config/{kind}/{id}": h.DeleteObject,
		"GET /api/v1/profiles":              h.Profiles,
	}
	for p, hd := range routes {
		mux.Handle(p, g.Admin(hd))
	}
}
