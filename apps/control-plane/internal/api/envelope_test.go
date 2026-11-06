package api

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Clients read data unconditionally, so an empty payload must still be sent:
// an empty list as [], not left out.
func TestSuccessResponseAlwaysSendsData(t *testing.T) {
	for name, tc := range map[string]struct {
		resp any
		want string
	}{
		"an empty list": {SuccessResponse(nil, []string{}), `{"status":"success","data":[]}`},
		"a nil list":    {SuccessResponse[[]string](nil, nil), `{"status":"success","data":null}`},
		"a zero value":  {SuccessResponse(nil, false), `{"status":"success","data":false}`},
	} {
		t.Run(name, func(t *testing.T) {
			b, err := json.Marshal(tc.resp)
			require.NoError(t, err)
			assert.JSONEq(t, tc.want, string(b))
		})
	}
}
