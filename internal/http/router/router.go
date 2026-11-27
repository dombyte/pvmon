// Package router declares every HTTP route and its access level in one place.
package router

import (
	"net/http"
	"slices"
)

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
	Docs    http.Handler
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
	for _, r := range routes(h) {
		hd := r.handler
		switch r.access {
		case accessRead:
			hd = g.Read(hd)
		case accessAdmin:
			hd = g.Admin(hd)
		case accessOpen:
		}
		mux.Handle(r.pattern, hd)
	}
	return mux
}

// Patterns returns the pattern of every route, for the OpenAPI contract test.
func Patterns() []string {
	rs := routes(Handlers{})
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.pattern
	}
	return out
}

type access int

const (
	accessOpen access = iota
	accessRead
	accessAdmin
)

type route struct {
	pattern string
	access  access
	handler http.Handler
}

func routes(h Handlers) []route {
	return slices.Concat(public(h), read(h), open(h.Auth), admin(h.Auth), config(h.Config))
}

func public(h Handlers) []route {
	return []route{
		{"GET /health", accessOpen, h.Health},
		{"GET /api/v1/version", accessOpen, h.Version},
		{"GET /docs/", accessOpen, h.Docs},
		{"GET /", accessOpen, h.UI},
	}
}

func read(h Handlers) []route {
	return []route{
		{"GET /api/v1/site", accessRead, h.Site},
		{"GET /api/v1/energy", accessRead, h.Energy},
		{"GET /api/v1/gauge", accessRead, h.Gauge},
		{"GET /api/v1/report", accessRead, h.Report},
		{"GET /api/v1/devices", accessRead, h.Devices.List},
		{"GET /api/v1/devices/{id}", accessRead, h.Devices.Get},
		{"GET /api/v1/devices/{id}/status", accessRead, h.Devices.Status},
		{"GET /api/v1/status", accessRead, h.Devices.Overview},
		{"GET /ws", accessRead, h.WS},
	}
}

// open routes need no login; logout checks its session itself.
func open(h AuthHandlers) []route {
	return []route{
		{"POST /api/v1/auth/login", accessOpen, h.Login},
		{"POST /api/v1/auth/logout", accessOpen, h.Logout},
		{"GET /api/v1/auth/me", accessOpen, h.Me},
		{"POST /api/v1/setup", accessOpen, h.Setup},
	}
}

func admin(h AuthHandlers) []route {
	return []route{
		{"GET /api/v1/users", accessAdmin, h.Users},
		{"POST /api/v1/users", accessAdmin, h.AddUser},
		{"DELETE /api/v1/users/{id}", accessAdmin, h.DeleteUser},
		{"PUT /api/v1/users/{id}/password", accessAdmin, h.SetPassword},
		{"GET /api/v1/tokens", accessAdmin, h.Tokens},
		{"POST /api/v1/tokens", accessAdmin, h.CreateToken},
		{"DELETE /api/v1/tokens/{id}", accessAdmin, h.RevokeToken},
		{"GET /api/v1/audit", accessAdmin, h.AuditLog},
	}
}

func config(h ConfigHandlers) []route {
	return []route{
		{"GET /api/v1/config", accessAdmin, h.Overview},
		{"PUT /api/v1/config", accessAdmin, h.PutObjects},
		{"POST /api/v1/config/probe", accessAdmin, h.Probe},
		{"GET /api/v1/config/settings", accessAdmin, h.Settings},
		{"PUT /api/v1/config/settings", accessAdmin, h.PutSettings},
		{"GET /api/v1/config/export", accessAdmin, h.Export},
		{"GET /api/v1/config/{kind}/{id}", accessAdmin, h.Object},
		{"PUT /api/v1/config/{kind}/{id}", accessAdmin, h.PutObject},
		{"DELETE /api/v1/config/{kind}/{id}", accessAdmin, h.DeleteObject},
		{"GET /api/v1/profiles", accessAdmin, h.Profiles},
	}
}
