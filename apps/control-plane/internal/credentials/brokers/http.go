package brokers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/ivaldibuild/ivaldi-control-plane/internal/credentials"
)

// call sends a JSON request to a store and decodes a 2xx answer into out,
// when out is set. It maps a transport failure, 429, and 5xx to
// ErrStoreUnavailable and other refusals to ErrStoreRefused, and returns the
// status for the caller to tell more apart. Its errors name the store's
// answer, never the request; a refusal or outage is an *answerError holding
// the start of the answer's body.
func call(ctx context.Context, client *http.Client, method, url string, header http.Header, body, out any) (int, error) {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return 0, err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", credentials.ErrStoreUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		sentinel := credentials.ErrStoreRefused
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			sentinel = credentials.ErrStoreUnavailable
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return resp.StatusCode, &answerError{status: resp.StatusCode, body: body, err: fmt.Errorf("%w: answered %d", sentinel, resp.StatusCode)}
	}
	if out != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, fmt.Errorf("%w: unreadable answer", credentials.ErrStoreUnavailable)
		}
	}
	return resp.StatusCode, nil
}
