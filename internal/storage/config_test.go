package storage

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cfgObj(kind, id, body, by string) ConfigObject {
	return ConfigObject{Kind: kind, ID: id, Body: body, UpdatedAt: 100, UpdatedBy: by}
}

func TestConfigEmptyState(t *testing.T) {
	t.Parallel()
	s, _ := newStorage(t)
	st, err := s.Config().ConfigState(context.Background())
	require.NoError(t, err)
	assert.Zero(t, st.Rev)
	assert.Empty(t, st.Objects)
	assert.NotNil(t, st.Settings)
	assert.Empty(t, st.Settings)
}

func TestConfigApply(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, db := newStorage(t)
	c := s.Config()

	rev, err := c.ApplyConfig(ctx, ConfigChange{
		Put: []ConfigObject{
			cfgObj("meter", "m1", `{"a":1}`, ""),
			cfgObj("device", "z", `{}`, "alice"),
			cfgObj("device", "a", `{}`, "alice"),
		},
		Set: map[string]string{"site.name": `"Home"`, "site.tz": `"UTC"`},
		At:  200, By: "alice", Snapshot: `{"v":1}`,
		Audit: AuditEntry{At: 200, Actor: "alice", Action: "config_change", Detail: "{}"},
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, rev)

	st, err := c.ConfigState(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, st.Rev)
	require.Len(t, st.Objects, 3)
	assert.Equal(t, "a", st.Objects[0].ID)
	assert.Equal(t, "z", st.Objects[1].ID)
	assert.Equal(t, "alice", st.Objects[0].UpdatedBy)
	assert.Equal(t, cfgObj("meter", "m1", `{"a":1}`, ""), st.Objects[2])
	assert.Equal(t, map[string]string{"site.name": `"Home"`, "site.tz": `"UTC"`}, st.Settings)

	var by sql.NullString
	require.NoError(t, db.QueryRow(
		`SELECT updated_by FROM config_object WHERE id='m1'`).Scan(&by))
	assert.False(t, by.Valid, "empty updated_by is stored as NULL")

	var at int64
	var snap string
	require.NoError(t, db.QueryRow(
		`SELECT at, by, snapshot FROM config_history WHERE rev=1`).Scan(&at, &by, &snap))
	assert.EqualValues(t, 200, at)
	assert.Equal(t, "alice", by.String)
	assert.JSONEq(t, `{"v":1}`, snap)
	assert.Equal(t, 1, countRows(t, db, "audit_log"))

	// upsert, delete, unset
	rev, err = c.ApplyConfig(ctx, ConfigChange{
		BaseRev: 1,
		Put:     []ConfigObject{cfgObj("device", "a", `{"x":2}`, "bob")},
		Delete:  []ConfigKey{{Kind: "device", ID: "z"}},
		Set:     map[string]string{"site.name": `"Flat"`},
		Unset:   []string{"site.tz"},
		At:      300, Snapshot: `{}`, Audit: audit("config_change", 300),
	})
	require.NoError(t, err)
	assert.EqualValues(t, 2, rev)
	st, err = c.ConfigState(ctx)
	require.NoError(t, err)
	require.Len(t, st.Objects, 2)
	assert.Equal(t, `{"x":2}`, st.Objects[0].Body)
	assert.Equal(t, "bob", st.Objects[0].UpdatedBy)
	assert.Equal(t, map[string]string{"site.name": `"Flat"`}, st.Settings)
	require.NoError(t, db.QueryRow(`SELECT by FROM config_history WHERE rev=2`).Scan(&by))
	assert.False(t, by.Valid)
	assert.Equal(t, 2, countRows(t, db, "audit_log"))
}

func TestConfigApplyConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, db := newStorage(t)
	c := s.Config()
	_, err := c.ApplyConfig(ctx, ConfigChange{
		Set: map[string]string{"k": "1"}, Snapshot: `{}`, Audit: audit("config_change", 1),
	})
	require.NoError(t, err)

	_, err = c.ApplyConfig(ctx, ConfigChange{
		BaseRev: 0, Put: []ConfigObject{cfgObj("device", "d", `{}`, "")},
		Set: map[string]string{"k": "2"}, Snapshot: `{}`, Audit: audit("config_change", 2),
	})
	require.ErrorIs(t, err, ErrConflict)

	st, err := c.ConfigState(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 1, st.Rev)
	assert.Empty(t, st.Objects)
	assert.Equal(t, map[string]string{"k": "1"}, st.Settings)
	assert.Equal(t, 1, countRows(t, db, "config_history"))
	assert.Equal(t, 1, countRows(t, db, "audit_log"))
}

// Every read and write step reports a failing statement as an error.
func TestConfigErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ok := ConfigChange{
		Put: []ConfigObject{cfgObj("device", "a", `{}`, "")}, Delete: []ConfigKey{{"meter", "m"}},
		Set: map[string]string{"site.name": `"x"`}, Unset: []string{"site.tz"},
		Snapshot: `{}`, Audit: AuditEntry{At: 1, Action: "config_change"},
	}
	for _, table := range []string{
		"config_history", "config_object", "config_setting", "audit_log",
	} {
		s, db := newStorage(t)
		_, err := db.ExecContext(ctx, `DROP TABLE `+table)
		require.NoError(t, err)
		_, err = s.Config().ApplyConfig(ctx, ok)
		require.Error(t, err, table)
		if table != "audit_log" {
			_, err = s.Config().ConfigState(ctx)
			require.Error(t, err, table)
		}
	}
	s, db := newStorage(t)
	_, err := db.ExecContext(ctx, `INSERT INTO config_setting VALUES('k', 'v');
		DROP TABLE config_setting; CREATE TABLE config_setting (key TEXT, value TEXT,
		extra TEXT)`)
	require.NoError(t, err)
	_, err = s.Config().ConfigState(ctx)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	_, err = s.Config().ConfigState(ctx)
	require.Error(t, err)
}
