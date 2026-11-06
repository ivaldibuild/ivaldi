package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type inner struct {
	Tags []string `json:"tags"`
}

type body struct {
	Items    []inner           `json:"items"`
	Nested   *inner            `json:"nested"`
	Missing  *inner            `json:"missing"`
	Raw      json.RawMessage   `json:"raw"`
	Labels   map[string]string `json:"labels"`
	ByKey    map[string]inner  `json:"by_key"`
	Any      any               `json:"any"`
	When     time.Time         `json:"when"`
	internal []string
}

func TestEmptyLists(t *testing.T) {
	in := Response[body]{Data: body{
		Items:  []inner{{}},
		Nested: &inner{},
		ByKey:  map[string]inner{"a": {}},
		Any:    inner{},
		When:   time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC),
	}}
	out, err := emptyLists(nil, "200", in)
	require.NoError(t, err)
	b, err := json.Marshal(out)
	require.NoError(t, err)
	assert.JSONEq(t, `{"status":"","data":{
		"items":[{"tags":[]}], "nested":{"tags":[]}, "missing":null, "raw":null,
		"labels":null, "by_key":{"a":{"tags":[]}}, "any":{"tags":[]}, "when":"2026-11-06T12:00:00Z"}}`, string(b))

	assert.Nil(t, in.Data.Items[0].Tags, "the original is left alone")
	assert.Nil(t, in.Data.Nested.Tags)

	empty, err := emptyLists(nil, "200", Response[[]string]{})
	require.NoError(t, err)
	b, _ = json.Marshal(empty)
	assert.JSONEq(t, `{"status":"","data":[]}`, string(b), "a nil list at the top becomes []")
}
