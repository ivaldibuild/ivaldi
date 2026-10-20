package id

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type prefixedEntity struct{ ID string }

func (prefixedEntity) Prefix() string { return "tsp" }

type plainEntity struct{ ID string }

func init() {
	MustRegister(prefixedEntity{}.Prefix(), "test.prefixed")
}

func TestRegister(t *testing.T) {
	require.NoError(t, Register("tsa", "test.a"))
	require.NoError(t, Register("tsa", "test.a"), "registering again for the same owner does nothing")

	err := Register("tsa", "test.b")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already used by test.a")

	for _, bad := range []string{"", "ts", "tsxx", "TSA", "t5a"} {
		assert.Error(t, Register(bad, "test.bad"), "prefix %q", bad)
	}

	assert.Panics(t, func() { MustRegister("tsa", "test.c") })
}

func TestNew(t *testing.T) {
	got := New("tsp")

	assert.Len(t, got, 30, "prefixed IDs must fit the varchar(30) primary-key columns")
	assert.True(t, Valid("tsp", got))
	assert.NotEqual(t, got, New("tsp"))
	assert.Panics(t, func() { New("zzz") }, "an unregistered prefix is a programming error")
}

func TestFor(t *testing.T) {
	prefixed := For(&prefixedEntity{})
	assert.True(t, Valid("tsp", prefixed), "an entity with a Prefix gets a prefixed ID: %q", prefixed)

	plain := For(&plainEntity{})
	assert.Len(t, plain, 26)
	assert.True(t, ValidULID(plain), "an entity without a Prefix gets a plain ULID: %q", plain)
}

func TestAssign(t *testing.T) {
	e := &plainEntity{}
	Assign(&e.ID, e)
	assert.True(t, ValidULID(e.ID))

	existing := e.ID
	Assign(&e.ID, e)
	assert.Equal(t, existing, e.ID, "an existing ID is left alone")

	p := &prefixedEntity{}
	Assign(&p.ID, p)
	assert.True(t, Valid("tsp", p.ID))
}

func TestValid(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		id     string
		want   bool
	}{
		{"well formed", "wsp", "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV", true},
		{"wrong prefix", "prj", "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV", false},
		{"plain ULID", "wsp", "01ARZ3NDEKTSV4RRFFQ69G5FAV", false},
		{"lowercase ULID", "wsp", "wsp_01arz3ndektsv4rrffq69g5fav", false},
		{"ambiguous letter I", "wsp", "wsp_01ARZ3NDEKTSV4RRFFQ69G5FAI", false},
		{"too short", "wsp", "wsp_01ARZ3NDEKTSV4RRFFQ69G5FA", false},
		{"missing separator", "wsp", "wsp01ARZ3NDEKTSV4RRFFQ69G5FAV", false},
		{"empty", "wsp", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Valid(tt.prefix, tt.id))
		})
	}

	assert.True(t, ValidULID("01ARZ3NDEKTSV4RRFFQ69G5FAV"))
	assert.False(t, ValidULID("wsp_01ARZ3NDEKTSV4RRFFQ69G5FAV"), "a prefixed ID is not a plain ULID")
}
