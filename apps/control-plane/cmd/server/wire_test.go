package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The guide prints helm --version with this, so only a version a chart was
// published under may come out; anything else leaves the version out.
func TestChartVersion(t *testing.T) {
	for binary, want := range map[string]string{
		"v0.4.0":                  "0.4.0",
		"0.4.0":                   "0.4.0",
		"v0.4.0-3-gabc1234":       "0.4.0-3-gabc1234",
		"v1.2.3-rc.1+build.5":     "1.2.3-rc.1+build.5",
		"dev":                     "",
		"abc1234":                 "",
		"v0.4.0-3-gabc1234-dirty": "",
		"v01.2.3":                 "",
		"":                        "",
	} {
		assert.Equal(t, want, chartVersion(binary), "binary %q", binary)
	}
}
