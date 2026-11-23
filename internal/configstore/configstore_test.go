package configstore

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/configstore/mocks"
	"github.com/dombyte/pvmon/internal/storage"
	"github.com/dombyte/pvmon/internal/util"
	"github.com/dombyte/pvmon/internal/util/clocktest"
)

var t0 = time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC)

var errBoom = errors.New("boom")

func testCipher(t *testing.T) *Cipher {
	t.Helper()
	c, err := NewCipher(bytes.Repeat([]byte{7}, 32), rand.Reader)
	require.NoError(t, err)
	return c
}

func testSettings() Settings {
	return Settings{
		Secrets:     map[string][]string{KindDevice: {"auth.password"}},
		SettingKeys: []string{"site.name", "site.tz"},
	}
}

func newSvc(t *testing.T, st Store, sealer Sealer) *Service {
	t.Helper()
	sv, err := New(testSettings(), Deps{Store: st, Sealer: sealer, Clock: clocktest.New(t0)})
	require.NoError(t, err)
	return sv
}

func deviceBody(pw string) map[string]any {
	return map[string]any{
		"connection": "lan",
		"auth":       map[string]any{"user": "admin", "password": pw},
	}
}

func TestLoadKey(t *testing.T) {
	t.Parallel()
	t.Run("create", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "secret.key")
		c, err := LoadKey(path, true, rand.Reader)
		require.NoError(t, err)
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Len(t, raw, 32)
		fi, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
		sealed, err := c.Seal("x")
		require.NoError(t, err)

		again, err := LoadKey(path, false, rand.Reader)
		require.NoError(t, err)
		plain, err := again.Open(sealed)
		require.NoError(t, err)
		assert.Equal(t, "x", plain)
	})
	t.Run("missing without create", func(t *testing.T) {
		t.Parallel()
		c, err := LoadKey(filepath.Join(t.TempDir(), "none"), false, rand.Reader)
		require.NoError(t, err)
		_, err = c.Seal("x")
		require.ErrorIs(t, err, ErrNoKey)
		_, err = c.Open(encPrefix + "AAAA")
		require.ErrorIs(t, err, ErrNoKey)
	})
	t.Run("wrong length", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "secret.key")
		require.NoError(t, os.WriteFile(path, []byte("short"), 0o600))
		_, err := LoadKey(path, true, rand.Reader)
		require.Error(t, err)
	})
	t.Run("rand fails", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "secret.key")
		_, err := LoadKey(path, true, iotest.ErrReader(errBoom))
		require.ErrorIs(t, err, errBoom)
		assert.NoFileExists(t, path)
	})
	t.Run("unwritable", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "no-dir", "secret.key")
		_, err := LoadKey(path, true, rand.Reader)
		require.Error(t, err)
	})
	t.Run("unreadable path", func(t *testing.T) {
		t.Parallel()
		_, err := LoadKey(t.TempDir(), true, rand.Reader)
		require.Error(t, err)
	})
}

func TestCipher(t *testing.T) {
	t.Parallel()
	c := testCipher(t)
	sealed, err := c.Seal("hunter2")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(sealed, "enc:v1:"))
	assert.NotContains(t, sealed, "hunter2")
	plain, err := c.Open(sealed)
	require.NoError(t, err)
	assert.Equal(t, "hunter2", plain)

	other, err := NewCipher(bytes.Repeat([]byte{9}, 32), rand.Reader)
	require.NoError(t, err)
	for name, in := range map[string]string{
		"garbage":    "enc:v1:!!!not base64",
		"no prefix":  strings.TrimPrefix(sealed, encPrefix),
		"truncated":  encPrefix + "AAAA",
		"empty":      "",
		"tampered":   sealed[:len(sealed)-4] + "AAAA",
		"other key":  sealed,
		"wrong vers": "enc:v2:" + strings.TrimPrefix(sealed, encPrefix),
	} {
		cc := c
		if name == "other key" {
			cc = other
		}
		_, err := cc.Open(in)
		require.ErrorIs(t, err, ErrBadSecret, name)
	}

	failing, err := NewCipher(bytes.Repeat([]byte{7}, 32), iotest.ErrReader(errBoom))
	require.NoError(t, err)
	_, err = failing.Seal("x")
	require.ErrorIs(t, err, errBoom)
}

func TestNew(t *testing.T) {
	t.Parallel()
	_, err := New(testSettings(), Deps{})
	var de *util.DependencyError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, []string{"store", "sealer", "clock"}, de.Missing)
}

func storedState(t *testing.T, c *Cipher) storage.ConfigState {
	t.Helper()
	sealed, err := c.Seal("secret")
	require.NoError(t, err)
	body, err := json.Marshal(deviceBody(sealed))
	require.NoError(t, err)
	return storage.ConfigState{
		Rev: 4,
		Objects: []storage.ConfigObject{
			{Kind: "device", ID: "wr1", Body: string(body), UpdatedAt: 5000, UpdatedBy: "bob"},
			{Kind: "device", ID: "bad", Body: `{"auth":{"password":"enc:v1:zzz"}}`},
			{Kind: "connection", ID: "lan", Body: `{"address":"1.2.3.4"}`},
		},
		Settings: map[string]string{"site.name": `"Home"`},
	}
}

func TestLoad(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	c := testCipher(t)
	st := mocks.NewMockStore(t)
	st.EXPECT().ConfigState(mock.Anything).Return(storedState(t, c), nil)
	snap, err := newSvc(t, st, c).Load(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 4, snap.Rev)
	assert.Equal(t, "Home", snap.Settings["site.name"])
	o := snap.Objects[Key{KindDevice, "wr1"}]
	assert.Equal(t, deviceBody("secret"), o.Body)
	assert.Equal(t, "bob", o.UpdatedBy)
	assert.Equal(t, time.UnixMilli(5000), o.UpdatedAt)
	assert.Empty(t, o.Unreadable)
	bad := snap.Objects[Key{KindDevice, "bad"}]
	assert.Equal(t, []string{"auth.password"}, bad.Unreadable)
	pw, _ := getPath(bad.Body, "auth.password")
	assert.Empty(t, pw)
	assert.Equal(t, "1.2.3.4", snap.Objects[Key{KindConnection, "lan"}].Body["address"])
}

func TestLoadErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		state storage.ConfigState
		err   error
	}{
		"store":        {err: errBoom},
		"bad body":     {state: storage.ConfigState{Objects: []storage.ConfigObject{{Kind: "meter", ID: "m", Body: "{"}}}},
		"bad setting":  {state: storage.ConfigState{Settings: map[string]string{"k": "{"}}},
		"plain secret": {},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			st := mocks.NewMockStore(t)
			st.EXPECT().ConfigState(mock.Anything).Return(tt.state, tt.err)
			_, err := newSvc(t, st, testCipher(t)).Load(context.Background())
			if name == "plain secret" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			if tt.err != nil {
				require.ErrorIs(t, err, errBoom)
			}
		})
	}
}

func baseSnap() *Snapshot {
	return &Snapshot{
		Rev: 2,
		Objects: map[Key]Object{
			{KindDevice, "wr1"}: {Body: deviceBody("old"), UpdatedBy: "x"},
			{KindDevice, "lost"}: {
				Body: deviceBody(""), Unreadable: []string{"auth.password"},
			},
			{KindMeter, "grid"}: {Body: map[string]any{"device": "wr1"}},
		},
		Settings: map[string]any{"site.name": "Home"},
	}
}

func TestWithObject(t *testing.T) {
	t.Parallel()
	sv := newSvc(t, mocks.NewMockStore(t), testCipher(t))
	base := baseSnap()

	_, err := sv.WithObject(base, Key{"bogus", "x"}, nil, "u")
	require.ErrorIs(t, err, ErrUnknownKind)

	next, err := sv.WithObject(base, Key{KindDevice, "wr1"}, deviceBody(Masked), "alice")
	require.NoError(t, err)
	pw, _ := getPath(next.Objects[Key{KindDevice, "wr1"}].Body, "auth.password")
	assert.Equal(t, "old", pw)
	assert.Equal(t, "alice", next.Objects[Key{KindDevice, "wr1"}].UpdatedBy)
	assert.Equal(t, t0, next.Objects[Key{KindDevice, "wr1"}].UpdatedAt)

	next, err = sv.WithObject(base, Key{KindDevice, "wr1"}, deviceBody("new"), "alice")
	require.NoError(t, err)
	pw, _ = getPath(next.Objects[Key{KindDevice, "wr1"}].Body, "auth.password")
	assert.Equal(t, "new", pw)

	// copy on write: base is untouched
	pw, _ = getPath(base.Objects[Key{KindDevice, "wr1"}].Body, "auth.password")
	assert.Equal(t, "old", pw)
	assert.Equal(t, "x", base.Objects[Key{KindDevice, "wr1"}].UpdatedBy)
	assert.NotContains(t, base.Objects, Key{KindDevice, "fresh"})

	// object without the secret path is fine
	_, err = sv.WithObject(base, Key{KindMeter, "m"}, map[string]any{"device": "wr1"}, "u")
	require.NoError(t, err)

	tests := map[string]struct {
		key  Key
		pw   any
		path string
		msg  string
	}{
		"mask new object": {Key{KindDevice, "fresh"}, Masked, "devices.fresh.auth.password", "enter"},
		"mask unreadable": {Key{KindDevice, "lost"}, Masked, "devices.lost.auth.password", "enter"},
		"env reference":   {Key{KindDevice, "wr1"}, "${env:X}", "devices.wr1.auth.password", "references"},
		"file reference":  {Key{KindDevice, "wr1"}, "${file:/x}", "devices.wr1.auth.password", "references"},
		"non-string":      {Key{KindDevice, "wr1"}, 42, "devices.wr1.auth.password", "string"},
	}
	for name, tt := range tests {
		_, err := sv.WithObject(base, tt.key, map[string]any{
			"auth": map[string]any{"password": tt.pw},
		}, "u")
		var ve *ValidationError
		require.ErrorAs(t, err, &ve, name)
		require.Len(t, ve.Problems, 1, name)
		assert.Equal(t, tt.path, ve.Problems[0].Path, name)
		assert.Contains(t, ve.Problems[0].Message, tt.msg, name)
	}
}

func TestWithoutObject(t *testing.T) {
	t.Parallel()
	sv := newSvc(t, mocks.NewMockStore(t), testCipher(t))
	base := baseSnap()
	_, err := sv.WithoutObject(base, Key{"x", "y"})
	require.ErrorIs(t, err, ErrUnknownKind)
	_, err = sv.WithoutObject(base, Key{KindDevice, "nope"})
	require.ErrorIs(t, err, ErrNotFound)
	next, err := sv.WithoutObject(base, Key{KindMeter, "grid"})
	require.NoError(t, err)
	assert.NotContains(t, next.Objects, Key{KindMeter, "grid"})
	assert.Contains(t, base.Objects, Key{KindMeter, "grid"})
}

func TestWithSettings(t *testing.T) {
	t.Parallel()
	sv := newSvc(t, mocks.NewMockStore(t), testCipher(t))
	base := baseSnap()

	_, err := sv.WithSettings(base, map[string]any{"nope": 1, "also": 2, "site.tz": "UTC"})
	var ve *ValidationError
	require.ErrorAs(t, err, &ve)
	assert.Len(t, ve.Problems, 2)

	next, err := sv.WithSettings(base, map[string]any{"site.name": nil, "site.tz": "UTC"})
	require.NoError(t, err)
	assert.NotContains(t, next.Settings, "site.name")
	assert.Equal(t, "UTC", next.Settings["site.tz"])
	assert.Equal(t, "Home", base.Settings["site.name"])
	assert.NotContains(t, base.Settings, "site.tz")
}

func TestMaskedAndView(t *testing.T) {
	t.Parallel()
	sv := newSvc(t, mocks.NewMockStore(t), testCipher(t))
	o := Object{
		Body: deviceBody("pw"), UpdatedBy: "u", UpdatedAt: t0, Unreadable: []string{"a"},
	}
	m := sv.Masked(KindDevice, o)
	pw, _ := getPath(m, "auth.password")
	assert.Equal(t, Masked, pw)
	pw, _ = getPath(o.Body, "auth.password")
	assert.Equal(t, "pw", pw, "original is untouched")

	empty := sv.Masked(KindDevice, Object{Body: deviceBody("")})
	pw, _ = getPath(empty, "auth.password")
	assert.Empty(t, pw, "an empty secret stays empty")
	none := sv.Masked(KindDevice, Object{Body: map[string]any{"a": 1}})
	assert.Equal(t, map[string]any{"a": 1}, none)

	v := sv.View(Key{KindDevice, "wr1"}, o)
	assert.Equal(t, "device", v.Kind)
	assert.Equal(t, "wr1", v.ID)
	assert.False(t, v.Locked)
	assert.Equal(t, "u", v.UpdatedBy)
	assert.Equal(t, t0, *v.UpdatedAt)
	assert.Equal(t, []string{"a"}, v.Unreadable)
	pw, _ = getPath(v.Body, "auth.password")
	assert.Equal(t, Masked, pw)
}

func TestSnapshotLayerAndKeys(t *testing.T) {
	t.Parallel()
	s := baseSnap()
	s.Objects[Key{"bogus", "x"}] = Object{}
	s.Objects[Key{KindDevice, "a"}] = Object{Body: map[string]any{}}
	layer := s.Layer()
	assert.Equal(t, "Home", layer["site.name"])
	devs, ok := layer["devices"].(map[string]any)
	require.True(t, ok)
	assert.Len(t, devs, 3)
	assert.Equal(t, "wr1", layer["meters"].(map[string]any)["grid"].(map[string]any)["device"])
	assert.NotContains(t, layer, "bogus")
	assert.NotContains(t, layer, "connections")

	// layer bodies are copies
	devs["wr1"].(map[string]any)["auth"].(map[string]any)["password"] = "changed"
	pw, _ := getPath(s.Objects[Key{KindDevice, "wr1"}].Body, "auth.password")
	assert.Equal(t, "old", pw)

	assert.Equal(t, []Key{{KindDevice, "a"}, {KindDevice, "lost"}, {KindDevice, "wr1"}},
		s.Keys(KindDevice))
	assert.Empty(t, s.Keys(KindConnection))
}

func TestSection(t *testing.T) {
	t.Parallel()
	got, err := Section(KindConnection)
	require.NoError(t, err)
	assert.Equal(t, "connections", got)
	_, err = Section("x")
	require.ErrorIs(t, err, ErrUnknownKind)
}

func TestValidationErrorFormat(t *testing.T) {
	t.Parallel()
	e := &ValidationError{Problems: []Problem{{"a.b", "bad"}, {"c", "worse"}}}
	assert.Equal(t, "configstore: invalid: a.b: bad; c: worse", e.Error())
}

func captureApply(t *testing.T, rev int64, err error) (*mocks.MockStore, *storage.ConfigChange) {
	t.Helper()
	var got storage.ConfigChange
	st := mocks.NewMockStore(t)
	st.EXPECT().ApplyConfig(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, c storage.ConfigChange) (int64, error) {
			got = c
			return rev, err
		})
	return st, &got
}

func TestCommitNoChange(t *testing.T) {
	t.Parallel()
	st := mocks.NewMockStore(t) // any call fails the test
	sv := newSvc(t, st, testCipher(t))
	base := baseSnap()
	rev, err := sv.Commit(context.Background(), base, base.clone(), "u")
	require.NoError(t, err)
	assert.EqualValues(t, 2, rev)
}

func TestCommitChange(t *testing.T) {
	t.Parallel()
	c := testCipher(t)
	st, got := captureApply(t, 3, nil)
	sv := newSvc(t, st, c)
	base := baseSnap()
	for k, o := range base.Objects {
		o.stored = `{"kept":true}`
		base.Objects[k] = o
	}
	base.Settings["site.tz"] = "UTC"

	next, err := sv.WithObject(base, Key{KindDevice, "wr1"}, deviceBody("topsecret"), "alice")
	require.NoError(t, err)
	next, err = sv.WithoutObject(next, Key{KindMeter, "grid"})
	require.NoError(t, err)
	next, err = sv.WithSettings(next, map[string]any{"site.name": "Flat", "site.tz": nil})
	require.NoError(t, err)

	rev, err := sv.Commit(context.Background(), base, next, "alice")
	require.NoError(t, err)
	assert.EqualValues(t, 3, rev)

	assert.EqualValues(t, 2, got.BaseRev)
	assert.Equal(t, "alice", got.By)
	assert.Equal(t, t0.UnixMilli(), got.At)
	assert.Equal(t, []storage.ConfigKey{{Kind: KindMeter, ID: "grid"}}, got.Delete)
	assert.Equal(t, map[string]string{"site.name": `"Flat"`}, got.Set)
	assert.Equal(t, []string{"site.tz"}, got.Unset)

	require.Len(t, got.Put, 1)
	put := got.Put[0]
	assert.Equal(t, "wr1", put.ID)
	assert.Equal(t, "alice", put.UpdatedBy)
	assert.NotContains(t, put.Body, "topsecret")
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(put.Body), &body))
	sealed, _ := getPath(body, "auth.password")
	plain, err := c.Open(sealed.(string))
	require.NoError(t, err)
	assert.Equal(t, "topsecret", plain)

	assert.NotContains(t, got.Snapshot, "topsecret")
	var snap struct {
		Objects  map[string]map[string]json.RawMessage `json:"objects"`
		Settings map[string]any                        `json:"settings"`
	}
	require.NoError(t, json.Unmarshal([]byte(got.Snapshot), &snap))
	assert.JSONEq(t, put.Body, string(snap.Objects["devices"]["wr1"]))
	assert.JSONEq(t, `{"kept":true}`, string(snap.Objects["devices"]["lost"]))
	assert.NotContains(t, snap.Objects, "meters")
	assert.Equal(t, map[string]any{"site.name": "Flat"}, snap.Settings)

	assert.Equal(t, "config_change", got.Audit.Action)
	assert.Equal(t, "alice", got.Audit.Actor)
	assert.Equal(t, t0.UnixMilli(), got.Audit.At)
	assert.NotContains(t, got.Audit.Detail, "topsecret")
	assert.NotContains(t, got.Audit.Detail, "old")
	assert.Contains(t, got.Audit.Detail, `"***"`)
	var detail struct {
		Changes []change `json:"changes"`
	}
	require.NoError(t, json.Unmarshal([]byte(got.Audit.Detail), &detail))
	require.Len(t, detail.Changes, 3)
	assert.Equal(t, "delete", detail.Changes[1].Op)
	assert.Equal(t, "put", detail.Changes[0].Op)
	assert.Equal(t, [2]any{Masked, Masked}, detail.Changes[0].Changed["auth.password"])
	assert.Equal(t, "settings", detail.Changes[2].Op)
	assert.Equal(t, [2]any{"Home", "Flat"}, detail.Changes[2].Changed["site.name"])
}

func TestCommitNewObjectAndEmptySecret(t *testing.T) {
	t.Parallel()
	st, got := captureApply(t, 1, nil)
	sv := newSvc(t, st, testCipher(t))
	base := &Snapshot{Objects: map[Key]Object{}, Settings: map[string]any{}}
	next, err := sv.WithObject(base, Key{KindDevice, "d"}, deviceBody(""), "")
	require.NoError(t, err)
	_, err = sv.Commit(context.Background(), base, next, "")
	require.NoError(t, err)
	assert.NotContains(t, got.Put[0].Body, "enc:v1")
	assert.Empty(t, got.Unset)
}

func TestCommitErrors(t *testing.T) {
	t.Parallel()
	base := baseSnap()
	for k, o := range base.Objects {
		o.stored = `{}`
		base.Objects[k] = o
	}
	change := func(t *testing.T, sv *Service) *Snapshot {
		t.Helper()
		next, err := sv.WithObject(base, Key{KindDevice, "wr1"}, deviceBody("new"), "u")
		require.NoError(t, err)
		return next
	}
	t.Run("conflict", func(t *testing.T) {
		t.Parallel()
		st, _ := captureApply(t, 0, storage.ErrConflict)
		sv := newSvc(t, st, testCipher(t))
		_, err := sv.Commit(context.Background(), base, change(t, sv), "u")
		require.ErrorIs(t, err, ErrConflict)
	})
	t.Run("store", func(t *testing.T) {
		t.Parallel()
		st, _ := captureApply(t, 0, errBoom)
		sv := newSvc(t, st, testCipher(t))
		_, err := sv.Commit(context.Background(), base, change(t, sv), "u")
		require.ErrorIs(t, err, errBoom)
		require.NotErrorIs(t, err, ErrConflict)
	})
	t.Run("seal", func(t *testing.T) {
		t.Parallel()
		sealer := mocks.NewMockSealer(t)
		sealer.EXPECT().Seal("new").Return("", errBoom)
		sv := newSvc(t, mocks.NewMockStore(t), sealer)
		_, err := sv.Commit(context.Background(), base, change(t, sv), "u")
		require.ErrorIs(t, err, errBoom)
	})
	t.Run("no key", func(t *testing.T) {
		t.Parallel()
		c, err := LoadKey(filepath.Join(t.TempDir(), "none"), false, rand.Reader)
		require.NoError(t, err)
		sv := newSvc(t, mocks.NewMockStore(t), c)
		_, err = sv.Commit(context.Background(), base, change(t, sv), "u")
		require.ErrorIs(t, err, ErrNoKey)
	})
}
