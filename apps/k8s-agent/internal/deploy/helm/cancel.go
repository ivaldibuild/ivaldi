package helm

import (
	"context"
	"io"
	"net/http"
)

// boundTransport ties every request to ctx: once ctx ends, requests in
// flight are cancelled and new ones fail at once. Helm 4's actions run their
// writes in a goroutine that ignores the context RunWithContext was given,
// and its registry pull uses context.Background, so the context is applied
// where the requests are made instead.
type boundTransport struct {
	next http.RoundTripper
	ctx  context.Context
}

func bind(ctx context.Context, next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	return &boundTransport{next: next, ctx: ctx}
}

func (t *boundTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.ctx.Err(); err != nil {
		return nil, err
	}
	reqCtx, cancel := context.WithCancel(req.Context())
	stop := context.AfterFunc(t.ctx, cancel)
	resp, err := t.next.RoundTrip(req.WithContext(reqCtx))
	if err != nil {
		stop()
		cancel()
		return nil, err
	}
	// The body is read after RoundTrip returns, so the request stays bound
	// until it is closed.
	resp.Body = &boundBody{ReadCloser: resp.Body, done: func() { stop(); cancel() }}
	return resp, nil
}

type boundBody struct {
	io.ReadCloser
	done func()
}

func (b *boundBody) Close() error {
	err := b.ReadCloser.Close()
	b.done()
	return err
}
