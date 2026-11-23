package handler_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/configstore"
	"github.com/dombyte/pvmon/internal/http/handler"
	"github.com/dombyte/pvmon/internal/http/handler/mocks"
	"github.com/dombyte/pvmon/internal/service"
)

func configHandler(t *testing.T) (*handler.Config, *mocks.MockConfigService, *mocks.MockProfileLister) {
	t.Helper()
	svc := mocks.NewMockConfigService(t)
	pl := mocks.NewMockProfileLister(t)
	return handler.NewConfig(svc, pl, zerolog.Nop()), svc, pl
}

func kindID(kind, id string) func(*http.Request) *http.Request {
	return func(r *http.Request) *http.Request {
		r.SetPathValue("kind", kind)
		r.SetPathValue("id", id)
		return asAdmin(r)
	}
}

var cfgErrors = []struct {
	name   string
	err    error
	status int
	code   string
}{
	{"not found", fmt.Errorf("x: %w", configstore.ErrNotFound), 404, "not_found"},
	{"unknown kind", configstore.ErrUnknownKind, 404, "not_found"},
	{"locked", configstore.ErrLocked, 409, "locked_by_file"},
	{"conflict", configstore.ErrConflict, 409, "conflict"},
	{"other", errors.New("boom"), 500, "internal"},
}

func TestConfigOverviewAndSettings(t *testing.T) {
	t.Parallel()
	ov := configstore.Overview{
		Rev:      7,
		Settings: map[string]configstore.SettingView{"a": {Value: 1, Stored: true}},
	}
	h, svc, _ := configHandler(t)
	svc.EXPECT().Overview(mock.Anything).Return(ov, nil).Twice()
	rec := call(h.Overview, http.MethodGet, "", asAdmin)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"config_rev":7`)
	rec = call(h.Settings, http.MethodGet, "", asAdmin)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t,
		`{"config_rev":7,"settings":{"a":{"value":1,"locked":false,"stored":true}}}`,
		rec.Body.String())
}

func TestConfigOverviewErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range cfgErrors {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, svc, _ := configHandler(t)
			svc.EXPECT().Overview(mock.Anything).Return(configstore.Overview{}, tc.err).Twice()
			for _, f := range []http.HandlerFunc{h.Overview, h.Settings} {
				rec := call(f, http.MethodGet, "", asAdmin)
				assert.Equal(t, tc.status, rec.Code)
				assert.Contains(t, rec.Body.String(), `"code":"`+tc.code+`"`)
			}
		})
	}
}

func TestConfigPutSettings(t *testing.T) {
	t.Parallel()
	h, svc, _ := configHandler(t)
	svc.EXPECT().PutSettings(mock.Anything, "user:root", map[string]any{"x": 1.0, "y": nil}).
		Return(configstore.Overview{Rev: 3}, nil)
	rec := call(h.PutSettings, http.MethodPut, `{"x":1,"y":null}`, asAdmin)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"config_rev":3`)
}

func TestConfigPutSettingsErrors(t *testing.T) {
	t.Parallel()
	h, svc, _ := configHandler(t)
	rec := call(h.PutSettings, http.MethodPut, `{bad`, asAdmin)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":"invalid_param"`)
	svc.EXPECT().PutSettings(mock.Anything, mock.Anything, mock.Anything).
		Return(configstore.Overview{}, &configstore.ValidationError{
			Problems: []configstore.Problem{{Path: "a.b", Message: "bad"}, {Path: "c", Message: "x"}},
		})
	rec = call(h.PutSettings, http.MethodPut, `{}`, asAdmin)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.JSONEq(t, `{"error":"Unprocessable Entity","code":"validation_failed",
		"message":"the configuration is invalid",
		"details":[{"path":"a.b","message":"bad"},{"path":"c","message":"x"}]}`,
		rec.Body.String())
}

func TestConfigObject(t *testing.T) {
	t.Parallel()
	h, svc, _ := configHandler(t)
	view := configstore.ObjectView{Kind: "devices", ID: "wr1", Body: map[string]any{"unit": 1}}
	svc.EXPECT().Object(mock.Anything, "devices", "wr1").Return(view, nil)
	svc.EXPECT().Overview(mock.Anything).Return(configstore.Overview{Rev: 9}, nil)
	rec := call(h.Object, http.MethodGet, "", kindID("devices", "wr1"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"config_rev":9`)
	assert.Contains(t, rec.Body.String(), `"id":"wr1"`)
}

func TestConfigObjectErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range cfgErrors {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, svc, _ := configHandler(t)
			svc.EXPECT().Object(mock.Anything, "devices", "a").
				Return(configstore.ObjectView{}, tc.err)
			rec := call(h.Object, http.MethodGet, "", kindID("devices", "a"))
			assert.Equal(t, tc.status, rec.Code)
			assert.Contains(t, rec.Body.String(), `"code":"`+tc.code+`"`)
		})
	}
	h, svc, _ := configHandler(t)
	svc.EXPECT().Object(mock.Anything, "devices", "a").Return(configstore.ObjectView{}, nil)
	svc.EXPECT().Overview(mock.Anything).Return(configstore.Overview{}, errors.New("boom"))
	rec := call(h.Object, http.MethodGet, "", kindID("devices", "a"))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestConfigPutObject(t *testing.T) {
	t.Parallel()
	h, svc, _ := configHandler(t)
	svc.EXPECT().PutObject(mock.Anything, "user:root", "devices", "wr1",
		map[string]any{"unit": 2.0}).
		Return(configstore.ObjectView{Kind: "devices", ID: "wr1"}, int64(12), nil)
	rec := call(h.PutObject, http.MethodPut, `{"unit":2}`, kindID("devices", "wr1"))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"config_rev":12`)

	rec = call(h.PutObject, http.MethodPut, `nope`, kindID("devices", "wr1"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid_param")
}

func TestConfigPutObjectErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range cfgErrors {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, svc, _ := configHandler(t)
			svc.EXPECT().PutObject(mock.Anything, mock.Anything, "devices", "a", mock.Anything).
				Return(configstore.ObjectView{}, int64(0), tc.err)
			rec := call(h.PutObject, http.MethodPut, `{}`, kindID("devices", "a"))
			assert.Equal(t, tc.status, rec.Code)
			assert.Contains(t, rec.Body.String(), `"code":"`+tc.code+`"`)
		})
	}
	h, svc, _ := configHandler(t)
	svc.EXPECT().PutObject(mock.Anything, mock.Anything, "devices", "a", mock.Anything).
		Return(configstore.ObjectView{}, int64(0), &configstore.ValidationError{
			Problems: []configstore.Problem{{Path: "unit", Message: "bad"}},
		})
	rec := call(h.PutObject, http.MethodPut, `{}`, kindID("devices", "a"))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), `"path":"unit"`)
}

func TestConfigDeleteObject(t *testing.T) {
	t.Parallel()
	h, svc, _ := configHandler(t)
	svc.EXPECT().DeleteObject(mock.Anything, "user:root", "devices", "wr1").Return(int64(4), nil)
	rec := call(h.DeleteObject, http.MethodDelete, "", kindID("devices", "wr1"))
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())

	for _, tc := range cfgErrors {
		svc.EXPECT().DeleteObject(mock.Anything, "user:root", "k", tc.name).
			Return(int64(0), tc.err)
		rec = call(h.DeleteObject, http.MethodDelete, "", kindID("k", tc.name))
		assert.Equal(t, tc.status, rec.Code, tc.name)
		assert.Contains(t, rec.Body.String(), `"code":"`+tc.code+`"`)
	}
}

func TestConfigExport(t *testing.T) {
	t.Parallel()
	h, svc, _ := configHandler(t)
	svc.EXPECT().Export(mock.Anything).Return([]byte("a: 1\n"), nil).Once()
	rec := call(h.Export, http.MethodGet, "", asAdmin)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.True(t, strings.HasPrefix(rec.Header().Get("Content-Type"), "application/yaml"))
	assert.Equal(t, `attachment; filename="config.yaml"`, rec.Header().Get("Content-Disposition"))
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	assert.Equal(t, "a: 1\n", rec.Body.String())

	svc.EXPECT().Export(mock.Anything).Return(nil, errors.New("boom")).Once()
	rec = call(h.Export, http.MethodGet, "", asAdmin)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestConfigProfiles(t *testing.T) {
	t.Parallel()
	h, _, pl := configHandler(t)
	pl.EXPECT().List().Return([]service.ProfileInfo{{ID: "solis/s6-hybrid", Vendor: "solis"}})
	rec := call(h.Profiles, http.MethodGet, "", asAdmin)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"id":"solis/s6-hybrid"`)
}

func TestConfigPutObjects(t *testing.T) {
	t.Parallel()
	h, svc, _ := configHandler(t)
	want := []configstore.Change{
		{Kind: "device", ID: "wr1", Body: map[string]any{"unit": float64(2)}},
		{Kind: "meter", ID: "old"},
	}
	svc.EXPECT().PutObjects(mock.Anything, "user:root", want).
		Return(configstore.Overview{Rev: 3}, nil).Once()
	rec := call(h.PutObjects, http.MethodPut, `{"objects":[`+
		`{"kind":"device","id":"wr1","body":{"unit":2}},{"kind":"meter","id":"old","body":null}]}`,
		asAdmin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"config_rev":3`)

	rec = call(h.PutObjects, http.MethodPut, `[`, asAdmin)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	svc.EXPECT().PutObjects(mock.Anything, "user:root", mock.Anything).
		Return(configstore.Overview{}, &configstore.ValidationError{
			Problems: []configstore.Problem{{Path: "meters", Message: "no grid"}},
		}).Once()
	rec = call(h.PutObjects, http.MethodPut, `{"objects":[]}`, asAdmin)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), `"path":"meters"`)
}

func TestConfigProbe(t *testing.T) {
	t.Parallel()
	h, svc, _ := configHandler(t)
	v := 3200.0
	res := service.ProbeResult{Device: "wr1", Blocks: []service.ProbeBlock{{
		Name: "pv", Role: "pv", Channels: []service.ProbeChannel{
			{Name: "power", Value: &v, Unit: "W"}, {Name: "energy_out", Unit: "Wh"},
		},
	}}}
	draft := []configstore.Change{{Kind: "device", ID: "wr1", Body: map[string]any{"unit": 1.0}}}
	svc.EXPECT().Probe(mock.Anything, "user:root", draft, "wr1").Return(res, nil).Once()
	rec := call(h.Probe, http.MethodPost,
		`{"objects":[{"kind":"device","id":"wr1","body":{"unit":1}}],"device":"wr1"}`, asAdmin)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"value":3200`)
	assert.Contains(t, rec.Body.String(), `{"name":"energy_out","value":null,"unit":"Wh"}`)

	rec = call(h.Probe, http.MethodPost, `{"objects":[]}`, asAdmin)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "device is required")
	rec = call(h.Probe, http.MethodPost, `x`, asAdmin)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	svc.EXPECT().Probe(mock.Anything, "user:root", mock.Anything, "ghost").
		Return(service.ProbeResult{}, configstore.ErrNotFound).Once()
	rec = call(h.Probe, http.MethodPost, `{"device":"ghost"}`, asAdmin)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
