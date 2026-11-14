package secrets

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
)

// minMasked is the shortest value masked: shorter ones would mask ordinary
// words, and the keyed fingerprint is what keeps them out of the control
// plane.
const minMasked = 4

// mask replaces a secret value.
const mask = "****"

// Masker holds the secret values currently in use and hides them in text.
type Masker struct {
	mu     sync.RWMutex
	values map[string]int
}

// NewMasker returns an empty Masker.
func NewMasker() *Masker { return &Masker{values: map[string]int{}} }

// Add masks values until Remove is called with them.
func (m *Masker) Add(values ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range values {
		if len(v) >= minMasked {
			m.values[v]++
		}
	}
}

// Remove stops masking values once every Add of them is removed.
func (m *Masker) Remove(values ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range values {
		if m.values[v] <= 1 {
			delete(m.values, v)
		} else {
			m.values[v]--
		}
	}
}

// Mask returns s with every secret value in it replaced.
func (m *Masker) Mask(s string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.values) == 0 {
		return s
	}
	// Longest first, so a value containing another is masked whole.
	values := make([]string, 0, len(m.values))
	for v := range m.values {
		values = append(values, v)
	}
	slices.SortFunc(values, func(a, b string) int { return len(b) - len(a) })
	for _, v := range values {
		s = strings.ReplaceAll(s, v, mask)
	}
	return s
}

// MaskAny masks the strings inside a report's detail.
func (m *Masker) MaskAny(v any) any {
	switch t := v.(type) {
	case string:
		return m.Mask(t)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, child := range t {
			out[k] = m.MaskAny(child)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, child := range t {
			out[i] = m.MaskAny(child)
		}
		return out
	case error:
		return m.Mask(t.Error())
	case fmt.Stringer:
		return m.Mask(t.String())
	default:
		return v
	}
}

// NewMaskingHandler wraps a slog handler so every message and attribute,
// including errors and nested groups, has secret values masked before it is
// written. Masking in the handler means a new log line cannot forget to.
func NewMaskingHandler(inner slog.Handler, m *Masker) slog.Handler {
	return &maskingHandler{inner: inner, masker: m}
}

type maskingHandler struct {
	inner  slog.Handler
	masker *Masker
}

func (h *maskingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *maskingHandler) Handle(ctx context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, h.masker.Mask(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(h.attr(a))
		return true
	})
	return h.inner.Handle(ctx, out)
}

func (h *maskingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	masked := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		masked[i] = h.attr(a)
	}
	return &maskingHandler{inner: h.inner.WithAttrs(masked), masker: h.masker}
}

func (h *maskingHandler) WithGroup(name string) slog.Handler {
	return &maskingHandler{inner: h.inner.WithGroup(name), masker: h.masker}
}

func (h *maskingHandler) attr(a slog.Attr) slog.Attr {
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return slog.String(a.Key, h.masker.Mask(v.String()))
	case slog.KindGroup:
		group := v.Group()
		masked := make([]any, len(group))
		for i, g := range group {
			masked[i] = h.attr(g)
		}
		return slog.Group(a.Key, masked...)
	case slog.KindAny:
		switch value := v.Any().(type) {
		case error, fmt.Stringer:
			return slog.String(a.Key, h.masker.MaskAny(value).(string))
		case map[string]any, []any:
			return slog.Any(a.Key, h.masker.MaskAny(value))
		}
		return slog.String(a.Key, h.masker.Mask(fmt.Sprint(v.Any())))
	}
	return a
}
