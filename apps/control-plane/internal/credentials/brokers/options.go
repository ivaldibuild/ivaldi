package brokers

import "net/http"

type Option func(*options)

type options struct {
	client     *http.Client
	dopplerAPI string
	roles      roleAssumer
}

// WithHTTPClient replaces the client stores' APIs are called with. It
// should follow no redirect.
func WithHTTPClient(c *http.Client) Option { return func(o *options) { o.client = c } }

// WithDopplerAPI replaces Doppler's API address, for tests.
func WithDopplerAPI(api string) Option { return func(o *options) { o.dopplerAPI = api } }
