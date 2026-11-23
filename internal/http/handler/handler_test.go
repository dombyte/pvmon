package handler_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/dombyte/pvmon/internal/buildinfo"
	"github.com/dombyte/pvmon/internal/http/handler"
	"github.com/dombyte/pvmon/internal/http/handler/mocks"
	"github.com/dombyte/pvmon/internal/service"
)

func TestHealthAndVersion(t *testing.T) {
	t.Parallel()
	svc := mocks.NewMockHealthService(t)
	svc.EXPECT().Report().Return(service.HealthReport{Status: "failed"},
		http.StatusServiceUnavailable)
	rec := httptest.NewRecorder()
	handler.NewHealth(svc, zerolog.Nop()).ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.JSONEq(t, `{"status":"failed","components":null,"devices":null}`, rec.Body.String())
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	rec = httptest.NewRecorder()
	v := handler.NewVersion(buildinfo.New("1.2.3-abc", "abc", "2026-11-19"), zerolog.Nop())
	v.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/version", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"version":"1.2.3-abc"`)
	assert.Contains(t, rec.Body.String(), `"build_date":"2026-11-19"`)

	v.ServeHTTP(failWriter{httptest.NewRecorder()}, httptest.NewRequest("GET", "/", nil))
}

type failWriter struct{ *httptest.ResponseRecorder }

func (failWriter) Write([]byte) (int, error) { return 0, http.ErrHandlerTimeout }

func TestSite(t *testing.T) {
	t.Parallel()
	svc := mocks.NewMockSiteService(t)
	svc.EXPECT().Model(mock.Anything).Return(service.SiteModel{Name: "Home"}, nil).Once()
	svc.EXPECT().Model(mock.Anything).Return(service.SiteModel{}, errors.New("db")).Once()
	h := handler.NewSite(svc, zerolog.Nop())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/site", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"name":"Home"`)

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/site", nil))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.JSONEq(t, `{"error":"Internal Server Error","code":"internal",
		"message":"the site model is not available"}`, rec.Body.String())
}
