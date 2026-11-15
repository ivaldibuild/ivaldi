package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The guide prints helm --version with this, so only a version a chart was
// published under may come out; anything else leaves the version out.
func TestAgentChartVersion(t *testing.T) {
	for pin, want := range map[string]string{
		"0.4.0":           "0.4.0",
		"v0.4.0":          "0.4.0",
		"0.4.0-rc.1":      "0.4.0-rc.1",
		" 0.4.0\n":        "0.4.0",
		"":                "",
		"0.0.0":           "",
		"dev":             "",
		"abc1234":         "",
		"0.4.0-dirty":     "",
		"v01.2.3":         "",
		"0.4.0-3-gabc123": "",
	} {
		assert.Equal(t, want, agentChartVersion(pin), "pin %q", pin)
	}
}
