package handler_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/auth"
	"github.com/dombyte/pvmon/internal/http/handler"
	"github.com/dombyte/pvmon/internal/http/handler/mocks"
	"github.com/dombyte/pvmon/internal/util/clocktest"
)

func authHandler(t *testing.T, public bool) (*handler.Auth, *mocks.MockAuthService) {
	t.Helper()
	svc := mocks.NewMockAuthService(t)
	clk := clocktest.New(time.Date(2026, 11, 19, 23, 30, 0, 0, time.UTC)) // 00:30 on the 20th
	h, err := handler.NewAuth(svc, public, clk, berlin, zerolog.Nop())
	require.NoError(t, err)
	return h, svc
}

func admin() auth.Principal { return auth.Principal{UserID: 1, Name: "root", Via: auth.ViaSession} }

func call(
	f http.HandlerFunc, method, body string, mod func(*http.Request) *http.Request,
) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	if mod != nil {
		req = mod(req)
	}
	rec := httptest.NewRecorder()
	f(rec, req)
	return rec
}

func asAdmin(r *http.Request) *http.Request {
	return r.WithContext(auth.WithPrincipal(r.Context(), admin()))
}

func withID(id string) func(*http.Request) *http.Request {
	return func(r *http.Request) *http.Request {
		r.SetPathValue("id", id)
		return asAdmin(r)
	}
}

func TestAuthDeps(t *testing.T) {
	t.Parallel()
	_, err := handler.NewAuth(nil, false, nil, nil, zerolog.Nop())
	require.Error(t, err)
}

func TestLogin(t *testing.T) {
	t.Parallel()
	exp := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		https  bool
		secure bool
	}{{"http", false, false}, {"https", true, true}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, svc := authHandler(t, false)
			svc.EXPECT().Login(mock.Anything, "ann", "pw", "1.2.3.4").Return(auth.Login{
				Token: "tok", Expires: exp, User: auth.UserInfo{ID: 2, Name: "ann"},
			}, nil)
			rec := call(h.Login, http.MethodPost, `{"name":"ann","password":"pw"}`,
				func(r *http.Request) *http.Request {
					return r.WithContext(auth.WithClient(r.Context(),
						auth.Client{IP: "1.2.3.4", HTTPS: tc.https}))
				})
			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Contains(t, rec.Body.String(), `"name":"ann"`)
			cs := rec.Result().Cookies()
			require.Len(t, cs, 1)
			assert.Equal(t, auth.SessionCookie, cs[0].Name)
			assert.Equal(t, "tok", cs[0].Value)
			assert.True(t, cs[0].HttpOnly)
			assert.Equal(t, http.SameSiteStrictMode, cs[0].SameSite)
			assert.Equal(t, tc.secure, cs[0].Secure)
		})
	}
}

func TestBadBody(t *testing.T) {
	t.Parallel()
	h, _ := authHandler(t, false)
	fs := map[string]http.HandlerFunc{
		"login": h.Login, "setup": h.Setup, "adduser": h.AddUser,
		"token": h.CreateToken, "password": h.SetPassword,
	}
	for name, f := range fs {
		for _, body := range []string{`{`, `{"bogus":1}`, ``} {
			rec := call(f, http.MethodPost, body, withID("1"))
			assert.Equal(t, http.StatusBadRequest, rec.Code, name+body)
			assert.Contains(t, rec.Body.String(), `"code":"invalid_param"`)
		}
	}
}

func TestErrorMapping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		err    error
		status int
		code   string
		extra  string
	}{
		{"bad credential", auth.ErrBadCredential, 401, "unauthorized", ""},
		{"unauthorized", auth.ErrUnauthorized, 401, "unauthorized", ""},
		{
			"rate limit", &auth.RateLimitError{RetryAfter: 1500 * time.Millisecond}, 429,
			"rate_limited", "",
		},
		{
			"password", &auth.PasswordError{Reason: "too short"}, 422, "validation_failed",
			"too short",
		},
		{"setup code", auth.ErrSetupCode, 403, "forbidden", ""},
		{"setup done", auth.ErrSetupDone, 409, "conflict", ""},
		{"name taken", auth.ErrNameTaken, 409, "conflict", ""},
		{"last user", auth.ErrLastUser, 409, "conflict", ""},
		{"not found", auth.ErrNotFound, 404, "not_found", ""},
		{"invalid", auth.ErrInvalid, 400, "invalid_param", ""},
		{"wrapped", errors.Join(errors.New("x"), auth.ErrNotFound), 404, "not_found", ""},
		{"other", errors.New("boom"), 500, "internal", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, svc := authHandler(t, false)
			svc.EXPECT().Login(mock.Anything, "a", "b", "").Return(auth.Login{}, tc.err)
			rec := call(h.Login, http.MethodPost, `{"name":"a","password":"b"}`, nil)
			assert.Equal(t, tc.status, rec.Code)
			assert.Contains(t, rec.Body.String(), `"code":"`+tc.code+`"`)
			assert.Contains(t, rec.Body.String(), tc.extra)
			assert.Empty(t, rec.Result().Cookies())
			if tc.status == 429 {
				assert.Equal(t, "2", rec.Header().Get("Retry-After"))
			}
			if tc.status == 422 {
				assert.Contains(t, rec.Body.String(), `"password"`)
			}
		})
	}
}

func TestSetup(t *testing.T) {
	t.Parallel()
	h, svc := authHandler(t, false)
	svc.EXPECT().Setup(mock.Anything, auth.SetupRequest{
		Code: "c", Name: "root", Password: "pw", Lang: "de", IP: "5.6.7.8",
	}).Return(auth.Login{Token: "t", User: auth.UserInfo{Name: "root"}}, nil)
	rec := call(h.Setup, http.MethodPost,
		`{"code":"c","name":"root","password":"pw","lang":"de"}`,
		func(r *http.Request) *http.Request {
			return r.WithContext(auth.WithClient(r.Context(), auth.Client{IP: "5.6.7.8"}))
		})
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Len(t, rec.Result().Cookies(), 1)

	svc.EXPECT().Setup(mock.Anything, mock.Anything).Return(auth.Login{}, auth.ErrSetupDone)
	rec = call(h.Setup, http.MethodPost, `{}`, nil)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestLogout(t *testing.T) {
	t.Parallel()
	h, svc := authHandler(t, false)
	with := func(p auth.Principal) func(*http.Request) *http.Request {
		return func(r *http.Request) *http.Request {
			return r.WithContext(auth.WithPrincipal(r.Context(), p))
		}
	}
	rec := call(h.Logout, http.MethodPost, "", nil)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	rec = call(h.Logout, http.MethodPost, "", with(auth.Principal{Via: auth.ViaToken}))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	svc.EXPECT().Logout(mock.Anything, admin()).Return(nil).Once()
	rec = call(h.Logout, http.MethodPost, "", with(admin()))
	assert.Equal(t, http.StatusNoContent, rec.Code)
	cs := rec.Result().Cookies()
	require.Len(t, cs, 1)
	assert.Equal(t, auth.SessionCookie, cs[0].Name)
	assert.Negative(t, cs[0].MaxAge)

	svc.EXPECT().Logout(mock.Anything, admin()).Return(errors.New("x")).Once()
	rec = call(h.Logout, http.MethodPost, "", with(admin()))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestMe(t *testing.T) {
	t.Parallel()
	h, svc := authHandler(t, true)
	svc.EXPECT().SetupRequired(mock.Anything).Return(true, nil).Once()
	rec := call(h.Me, http.MethodGet, "", nil)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"user":null,"setup_required":true,"public_read":true}`,
		rec.Body.String())

	svc.EXPECT().SetupRequired(mock.Anything).Return(false, nil).Once()
	rec = call(h.Me, http.MethodGet, "", asAdmin)
	assert.JSONEq(t, `{"user":{"id":1,"name":"root","via":"session"},`+
		`"setup_required":false,"public_read":true}`, rec.Body.String())

	svc.EXPECT().SetupRequired(mock.Anything).Return(false, errors.New("db")).Once()
	rec = call(h.Me, http.MethodGet, "", nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestUsers(t *testing.T) {
	t.Parallel()
	h, svc := authHandler(t, false)
	svc.EXPECT().Users(mock.Anything).Return([]auth.UserInfo{{ID: 1, Name: "root"}}, nil).Once()
	rec := call(h.Users, http.MethodGet, "", asAdmin)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"users":[`)

	svc.EXPECT().Users(mock.Anything).Return(nil, errors.New("x")).Once()
	rec = call(h.Users, http.MethodGet, "", asAdmin)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestAddUser(t *testing.T) {
	t.Parallel()
	h, svc := authHandler(t, false)
	svc.EXPECT().AddUser(mock.Anything, admin(), "bob", "pw", "en").
		Return(auth.UserInfo{ID: 2, Name: "bob"}, nil).Once()
	rec := call(h.AddUser, http.MethodPost, `{"name":"bob","password":"pw","lang":"en"}`,
		asAdmin)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Contains(t, rec.Body.String(), `"bob"`)

	svc.EXPECT().AddUser(mock.Anything, admin(), "bob", "", "").
		Return(auth.UserInfo{}, auth.ErrNameTaken).Once()
	rec = call(h.AddUser, http.MethodPost, `{"name":"bob"}`, asAdmin)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestDeleteUser(t *testing.T) {
	t.Parallel()
	h, svc := authHandler(t, false)
	svc.EXPECT().DeleteUser(mock.Anything, admin(), int64(7)).Return(nil).Once()
	rec := call(h.DeleteUser, http.MethodDelete, "", withID("7"))
	assert.Equal(t, http.StatusNoContent, rec.Code)

	rec = call(h.DeleteUser, http.MethodDelete, "", withID("x"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"id"`)

	svc.EXPECT().DeleteUser(mock.Anything, admin(), int64(8)).Return(auth.ErrLastUser).Once()
	rec = call(h.DeleteUser, http.MethodDelete, "", withID("8"))
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestSetPassword(t *testing.T) {
	t.Parallel()
	h, svc := authHandler(t, false)
	svc.EXPECT().SetPassword(mock.Anything, admin(), int64(3), "new").Return(nil).Once()
	rec := call(h.SetPassword, http.MethodPut, `{"password":"new"}`, withID("3"))
	assert.Equal(t, http.StatusNoContent, rec.Code)

	rec = call(h.SetPassword, http.MethodPut, `{"password":"new"}`, withID("x"))
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	svc.EXPECT().SetPassword(mock.Anything, admin(), int64(3), "pw").
		Return(&auth.PasswordError{Reason: "weak"}).Once()
	rec = call(h.SetPassword, http.MethodPut, `{"password":"pw"}`, withID("3"))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestTokens(t *testing.T) {
	t.Parallel()
	h, svc := authHandler(t, false)
	svc.EXPECT().Tokens(mock.Anything).Return([]auth.TokenInfo{{ID: "a"}}, nil).Once()
	rec := call(h.Tokens, http.MethodGet, "", asAdmin)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"tokens":[`)

	svc.EXPECT().Tokens(mock.Anything).Return(nil, errors.New("x")).Once()
	rec = call(h.Tokens, http.MethodGet, "", asAdmin)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestCreateToken(t *testing.T) {
	t.Parallel()
	h, svc := authHandler(t, false)
	svc.EXPECT().CreateToken(mock.Anything, admin(), "ci", 48*time.Hour).
		Return(auth.NewToken{TokenInfo: auth.TokenInfo{ID: "i"}, Token: "secret"}, nil).Once()
	rec := call(h.CreateToken, http.MethodPost, `{"name":"ci","expires_days":2}`, asAdmin)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Contains(t, rec.Body.String(), `"token":"secret"`)

	svc.EXPECT().CreateToken(mock.Anything, admin(), "x", time.Duration(0)).
		Return(auth.NewToken{}, auth.ErrInvalid).Once()
	rec = call(h.CreateToken, http.MethodPost, `{"name":"x"}`, asAdmin)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRevokeToken(t *testing.T) {
	t.Parallel()
	h, svc := authHandler(t, false)
	svc.EXPECT().RevokeToken(mock.Anything, admin(), "tid").Return(nil).Once()
	rec := call(h.RevokeToken, http.MethodDelete, "", withID("tid"))
	assert.Equal(t, http.StatusNoContent, rec.Code)

	svc.EXPECT().RevokeToken(mock.Anything, admin(), "nope").Return(auth.ErrNotFound).Once()
	rec = call(h.RevokeToken, http.MethodDelete, "", withID("nope"))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestAudit(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 19, 23, 30, 0, 0, time.UTC).In(berlin)
	d := func(day int) time.Time { return time.Date(2026, 11, day, 0, 0, 0, 0, berlin) }
	tests := []struct {
		name     string
		query    string
		from, to time.Time
	}{
		{"default", "", now.AddDate(0, 0, -30), now},
		{"explicit", "?from=2026-11-01&to=2026-11-10", d(1), d(11)},
		{"only from", "?from=2026-11-01", d(1), now},
		{
			"rfc3339", "?from=2026-11-01T10:00:00Z&to=2026-11-02", time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC),
			d(3),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h, svc := authHandler(t, false)
			svc.EXPECT().Audit(mock.Anything, mock.MatchedBy(tc.from.Equal),
				mock.MatchedBy(tc.to.Equal)).
				Return([]auth.AuditInfo{{ID: 1, Action: "login"}}, nil).Once()
			rec := call(h.Audit, http.MethodGet, "", func(r *http.Request) *http.Request {
				r.URL.RawQuery = strings.TrimPrefix(tc.query, "?")
				return asAdmin(r)
			})
			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Contains(t, rec.Body.String(), `"entries":[`)
		})
	}
}

func TestAuditErrors(t *testing.T) {
	t.Parallel()
	h, svc := authHandler(t, false)
	rec := call(h.Audit, http.MethodGet, "", func(r *http.Request) *http.Request {
		r.URL.RawQuery = "from=nope&to=bad"
		return r
	})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), `"from"`)
	assert.Contains(t, rec.Body.String(), `"to"`)

	svc.EXPECT().Audit(mock.Anything, mock.Anything, mock.Anything).
		Return(nil, errors.New("x")).Once()
	rec = call(h.Audit, http.MethodGet, "", nil)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
