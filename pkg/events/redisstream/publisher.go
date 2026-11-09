// Package redisstream publishes and consumes events over Redis streams
// (ADR 0009, ADR 0011).
//
// It is the transport side of pkg/events: domain code sees events.Envelope,
// events.Publisher, and events.Consumer, never Redis. A Relay forwards the
// outbox through a Publisher, and a Consumer reads a stream through its own
// consumer group, handling each entry with an events.DeliveryHandler.
//
// Every entry carries the envelope as flat string fields, so a consumer can
// route on type, deduplicate on id, and decode payload without knowing the
// publisher's version. See schemas/event-envelope.v1.json for the contract.
package redisstream

import (
	"context"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"

	"github.com/ivaldibuild/ivaldi/pkg/events"
)

// DefaultMaxLen caps a stream when the caller does not choose a cap. It must
// stay far above any expected backlog, because trimming can drop entries a
// consumer has not read yet.
const DefaultMaxLen int64 = 1_000_000

// Config holds a Publisher's collaborators.
type Config struct {
	// Client is the Redis connection. The caller owns TLS, ACLs, and
	// persistence, so the publisher never sees credentials.
	Client redis.UniversalClient
	// Stream is the stream events are added to.
	Stream string
	// MaxLen is the approximate maximum stream length; zero means
	// DefaultMaxLen.
	MaxLen int64
}

// Publisher adds event envelopes to a Redis stream with approximate trimming.
type Publisher struct {
	client redis.UniversalClient
	stream string
	maxLen int64
}

// NewPublisher returns a Publisher, or an error naming a missing setting.
func NewPublisher(c Config) (*Publisher, error) {
	if c.Client == nil {
		return nil, errors.New("redisstream: Client is required")
	}
	if c.Stream == "" {
		return nil, errors.New("redisstream: Stream is required")
	}
	maxLen := c.MaxLen
	if maxLen == 0 {
		maxLen = DefaultMaxLen
	}
	if maxLen < 0 {
		return nil, errors.New("redisstream: MaxLen must not be negative")
	}
	return &Publisher{client: c.Client, stream: c.Stream, maxLen: maxLen}, nil
}

// Publish adds the envelopes to the stream. A partial failure returns an
// error, so the caller retries the whole batch and consumers see duplicates;
// delivery is at least once. Every error comes from Redis, not from the
// envelopes, so it wraps events.ErrUnavailable.
func (p *Publisher) Publish(ctx context.Context, envelopes ...events.Envelope) error {
	if len(envelopes) == 0 {
		return nil
	}
	_, err := p.client.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		for _, e := range envelopes {
			pipe.XAdd(ctx, &redis.XAddArgs{
				Stream: p.stream,
				MaxLen: p.maxLen,
				Approx: true,
				Values: fields(e),
			})
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("publish %d event(s) to %s: %w: %w", len(envelopes), p.stream, events.ErrUnavailable, err)
	}
	return nil
}
