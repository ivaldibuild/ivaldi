package binding

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNames(t *testing.T) {
	names, err := Names("postgres://#{Database.Host}:5432/#{ Database.Name }?app=#{Database.Host}&literal=##{not}")
	require.NoError(t, err)
	assert.Equal(t, []string{"Database.Host", "Database.Name"}, names)

	_, err = Names("#{}")
	assert.ErrorIs(t, err, ErrEmpty)
	_, err = Names("#{Database.Name")
	assert.ErrorIs(t, err, ErrUnclosed)
}

func TestBind(t *testing.T) {
	system := System{EnvironmentSlug: "production", TargetSlug: "eu"}.Map()
	vars := []Variable{
		{Name: "Region", Value: "#{Ivaldi.Target.Slug}-1"},
		{Name: "Prefix", Value: "a##{b}"},
	}

	got, err := Bind(system, vars, []string{
		"payments/#{Ivaldi.Environment.Slug}/#{Region}/db",
		"#{Prefix}/db",
		"literal/##{kept}",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"payments/production/eu-1/db", "a#{b}/db", "literal/#{kept}"}, got,
		"a variable's own bindings are filled in, and ##{ is a literal #{ wherever it is written")

	_, err = Bind(system, []Variable{{Name: "A", Value: "#{B}"}, {Name: "B", Value: "#{A}"}}, nil)
	assert.ErrorContains(t, err, "cycle")
	_, err = Bind(system, nil, []string{"#{Missing}"})
	assert.ErrorContains(t, err, "#{Missing} is not defined")
	_, err = Bind(system, []Variable{{Name: EnvironmentSlug, Value: "x"}}, nil)
	assert.ErrorContains(t, err, "system variable")
}

// A path already filled in, escaped, comes back unchanged however many #{ it
// holds, so it can be sent where bindings are filled in again.
func TestEscape(t *testing.T) {
	for _, path := range []string{"payments/db", "a#{b}/db", "a##{b}", "#{", "x#", "###{y}"} {
		got, err := Substitute(Escape(path), func(string) (string, error) { return "", assert.AnError })
		require.NoError(t, err, path)
		assert.Equal(t, path, got)
	}
}

func TestSystemNamesAreTheMapsKeys(t *testing.T) {
	keys := make([]string, 0, len(SystemNames))
	for k := range (System{}).Map() {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	assert.Equal(t, SystemNames, keys)
}
