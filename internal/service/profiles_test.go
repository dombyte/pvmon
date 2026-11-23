package service_test

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dombyte/pvmon/internal/profile"
	"github.com/dombyte/pvmon/internal/service"
	"github.com/dombyte/pvmon/internal/service/mocks"
	"github.com/dombyte/pvmon/profiles"
)

func TestProfilesList(t *testing.T) {
	t.Parallel()
	src := mocks.NewMockProfileSource(t)
	src.EXPECT().IDs().Return([]string{"a/one", "a/bad", "b/two"})
	src.EXPECT().Load("a/one", "").Return(&profile.Loaded{Profile: &profile.Profile{
		ID: "a/one", Vendor: "a", Models: []string{"M"}, Transport: "modbus", Verified: true,
		Netting: "meter",
		Blocks: map[string]profile.Block{
			"zeta": {Role: "grid", RolesAllowed: []string{"grid", "load"}, Channels: map[string]profile.Channel{
				"q": {}, "b": {}, "m": {},
			}},
			"alpha": {Role: "pv", Category: "ac"},
		},
	}}, nil)
	src.EXPECT().Load("a/bad", "").Return(nil, errors.New("broken"))
	src.EXPECT().Load("b/two", mock.Anything).Return(&profile.Loaded{
		Profile: &profile.Profile{ID: "b/two"},
	}, nil)

	got := service.NewProfiles(src).List()
	require.Len(t, got, 2)
	assert.Equal(t, "a/one", got[0].ID)
	assert.True(t, got[0].Verified)
	assert.Equal(t, "meter", got[0].Netting)
	require.Len(t, got[0].Blocks, 2)
	assert.Equal(t, "alpha", got[0].Blocks[0].Name)
	assert.Equal(t, "ac", got[0].Blocks[0].Category)
	assert.Empty(t, got[0].Blocks[0].Channels)
	assert.Equal(t, "zeta", got[0].Blocks[1].Name)
	assert.Equal(t, []string{"b", "m", "q"}, got[0].Blocks[1].Channels)
	assert.Equal(t, []string{"grid", "load"}, got[0].Blocks[1].RolesAllowed)
	assert.Equal(t, "b/two", got[1].ID)
	assert.NotNil(t, got[1].Blocks)
}

func TestProfilesListEmbedded(t *testing.T) {
	t.Parallel()
	l, err := profile.NewLoader(profiles.FS(), os.ReadFile)
	require.NoError(t, err)
	var ids []string
	for _, p := range service.NewProfiles(l).List() {
		ids = append(ids, p.ID)
	}
	assert.NotEmpty(t, ids)
	assert.Contains(t, ids, "solis/s6-hybrid")
}
