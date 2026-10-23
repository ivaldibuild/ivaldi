package aggregate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ivaldibuild/ivaldi/pkg/aggregate"
	"github.com/ivaldibuild/ivaldi/pkg/id"
)

const widgetPrefix = "wid"

func init() {
	id.MustRegister(widgetPrefix, "aggregate_test.widget")
}

func TestNewEntityGeneratesAnIDForANewObject(t *testing.T) {
	prefixed, err := aggregate.NewEntity(widgetPrefix, nil)
	require.NoError(t, err)
	assert.True(t, id.Valid(widgetPrefix, prefixed.ID()), "prefixed: %s", prefixed.ID())

	plain, err := aggregate.NewEntity("", nil)
	require.NoError(t, err)
	assert.True(t, id.ValidULID(plain.ID()), "plain ULID: %s", plain.ID())

	other, err := aggregate.NewEntity(widgetPrefix, nil)
	require.NoError(t, err)
	assert.NotEqual(t, prefixed.ID(), other.ID())
	assert.True(t, prefixed.IsNew())
}

func TestNewEntityKeepsAStoredID(t *testing.T) {
	stored := id.New(widgetPrefix)
	ulid := id.ULID()
	tests := []struct {
		name    string
		prefix  string
		stored  string
		wantErr bool
	}{
		{name: "prefixed", prefix: widgetPrefix, stored: stored},
		{name: "plain ULID", stored: ulid},
		{name: "other prefix", prefix: "abc", stored: stored, wantErr: true},
		{name: "missing prefix", prefix: widgetPrefix, stored: ulid, wantErr: true},
		{name: "prefix where none is used", stored: stored, wantErr: true},
		{name: "empty", prefix: widgetPrefix, stored: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := aggregate.NewEntity(tt.prefix, &tt.stored)
			if tt.wantErr {
				assert.ErrorIs(t, err, aggregate.ErrInvalidID)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.stored, e.ID())
			assert.False(t, e.IsNew(), "a rebuilt object is not new")
		})
	}
}

func TestNewEntityPanicsOnAnUnregisteredPrefix(t *testing.T) {
	assert.Panics(t, func() { _, _ = aggregate.NewEntity("zzz", nil) })
}

func TestNewRoot(t *testing.T) {
	stored := id.New(widgetPrefix)

	r, err := aggregate.NewRoot(widgetPrefix, &stored)
	require.NoError(t, err)
	assert.Equal(t, stored, r.ID())
	assert.Empty(t, r.PullEvents())

	bad := "wid_nope"
	_, err = aggregate.NewRoot(widgetPrefix, &bad)
	assert.ErrorIs(t, err, aggregate.ErrInvalidID)
}
