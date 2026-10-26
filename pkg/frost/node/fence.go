package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/keep-network/keep-core/pkg/frost/store"
	"net/http"
	"time"
)

// localFence is a local rehearsal adapter to a separately owned authority.
// It is not a production authority implementation. Only public fence points
// cross this interface, never keys or worker records.
type localFence struct{ endpoint string }
type advance struct{ Before, After store.Point }

func (f localFence) call(ctx context.Context, method string, value, out any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, "POST", f.endpoint+"/"+method, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	response, err := (&http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("local fence redirect refused") }}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("local fence refused")
	}
	if out != nil {
		return json.NewDecoder(response.Body).Decode(out)
	}
	return nil
}
func (f localFence) Acquire(ctx context.Context, id [32]byte) (store.FenceLease, error) {
	return f, f.call(ctx, "acquire", id, nil)
}
func (f localFence) Head(ctx context.Context) (store.Point, error) {
	var p store.Point
	err := f.call(ctx, "head", nil, &p)
	return p, err
}
func (f localFence) Advance(ctx context.Context, before, after store.Point) error {
	return f.call(ctx, "advance", advance{before, after}, nil)
}
func (f localFence) Close() error { return f.call(context.Background(), "close", nil, nil) }
