// Package kafka produces and consumes HOTL events on Kafka, using franz-go.
//
// Events travel in CloudEvents structured mode: the record value is the whole envelope as JSON, the content-type
// header is application/cloudevents+json, and the key is the envelope's subject, so every event about one run,
// intent, brief, rule or repo lands on one partition, in order.
//
// The producer validates every event before sending it. The consumer is a tolerant reader: it decodes envelopes
// without validating them, so it keeps working when producers move to a newer contracts release first. Delivery is
// at least once; handlers dedupe on the envelope id.
package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/HumanOnLoop/contracts/events"
)

// Option configures a Producer or Consumer.
type Option func(*config)

type config struct {
	topicPrefix string
	clientOpts  []kgo.Opt
	onInvalid   func(*kgo.Record, error) error
}

// WithTopicPrefix prepends prefix to every topic name, for example to isolate tests on a shared cluster.
func WithTopicPrefix(prefix string) Option { return func(c *config) { c.topicPrefix = prefix } }

// WithClientOptions passes extra options to the franz-go client, such as TLS or SASL settings.
func WithClientOptions(opts ...kgo.Opt) Option {
	return func(c *config) { c.clientOpts = append(c.clientOpts, opts...) }
}

// WithInvalidRecordHandler sets what a Consumer does with a record that is not a HOTL envelope at all. (Events of a
// type this release does not know are delivered to the handler: a newer producer may add event types, and
// Message.Decode then returns an error wrapping events.ErrUnknownType.) Returning nil skips the record and commits past it; returning an error stops
// Run. Without a handler, Run stops with an error, so a bad record is never skipped silently.
func WithInvalidRecordHandler(fn func(rec *kgo.Record, err error) error) Option {
	return func(c *config) { c.onInvalid = fn }
}

func newConfig(opts []Option) config {
	var c config
	for _, o := range opts {
		o(&c)
	}
	return c
}

// NewRecord builds the Kafka record for an event, after validating it. Use it directly in an outbox relay, which
// stores envelopes in a transaction and publishes them later.
func NewRecord(env events.Envelope) (*kgo.Record, error) {
	if err := events.Validate(env); err != nil {
		return nil, fmt.Errorf("event %s (%s): %w", env.ID, env.Type, err)
	}
	topic, err := env.Topic()
	if err != nil {
		return nil, err
	}
	value, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	return &kgo.Record{
		Topic:   topic,
		Key:     []byte(env.Subject),
		Value:   value,
		Headers: []kgo.RecordHeader{{Key: "content-type", Value: []byte(events.ContentType)}},
	}, nil
}

// Producer publishes HOTL events. It is safe for concurrent use.
type Producer struct {
	client *kgo.Client
	prefix string
}

// NewProducer connects a producer to the given brokers. The franz-go defaults apply: idempotent writes, acks from
// all in-sync replicas, and the Kafka-compatible key partitioner.
func NewProducer(brokers []string, opts ...Option) (*Producer, error) {
	c := newConfig(opts)
	client, err := kgo.NewClient(append([]kgo.Opt{kgo.SeedBrokers(brokers...)}, c.clientOpts...)...)
	if err != nil {
		return nil, err
	}
	return &Producer{client: client, prefix: c.topicPrefix}, nil
}

// Publish validates every event and, only if all are valid, sends them and waits until Kafka has acknowledged
// each one. Events with the same subject are written in the order given.
func (p *Producer) Publish(ctx context.Context, envs ...events.Envelope) error {
	recs := make([]*kgo.Record, len(envs))
	for i, env := range envs {
		rec, err := NewRecord(env)
		if err != nil {
			return err
		}
		rec.Topic = p.prefix + rec.Topic
		recs[i] = rec
	}
	return p.client.ProduceSync(ctx, recs...).FirstErr()
}

// Close closes the connection. Publish waits for every record it sends, so nothing is left pending.
func (p *Producer) Close() { p.client.Close() }

// Message is an event received from Kafka.
type Message struct {
	events.Envelope
	Topic     string // as on the cluster, including any topic prefix
	Partition int32
	Offset    int64
}

// Handler processes one message. Returning an error stops the consumer without committing that message, so it is
// delivered again on the next run.
type Handler func(ctx context.Context, msg Message) error

// Consumer reads HOTL events as a member of a consumer group.
type Consumer struct {
	client    *kgo.Client
	onInvalid func(*kgo.Record, error) error
}

// NewConsumer joins the consumer group on the given topics (for example events.TopicRun). A new group starts at the
// beginning of each topic, so it sees the full history.
func NewConsumer(brokers []string, group string, topics []string, opts ...Option) (*Consumer, error) {
	if group == "" || len(topics) == 0 {
		return nil, errors.New("kafka: a consumer needs a group and at least one topic")
	}
	c := newConfig(opts)
	prefixed := make([]string, len(topics))
	for i, t := range topics {
		prefixed[i] = c.topicPrefix + t
	}
	base := []kgo.Opt{
		kgo.SeedBrokers(brokers...),
		kgo.ConsumerGroup(group),
		kgo.ConsumeTopics(prefixed...),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		kgo.DisableAutoCommit(),
	}
	client, err := kgo.NewClient(append(base, c.clientOpts...)...)
	if err != nil {
		return nil, err
	}
	return &Consumer{client: client, onInvalid: c.onInvalid}, nil
}

// Run delivers messages to h until ctx is cancelled, h returns an error, or a fetch fails. Offsets are committed
// only after h has handled a message, so every message is handled at least once. It returns nil when ctx is
// cancelled or the consumer is closed.
func (c *Consumer) Run(ctx context.Context, h Handler) error {
	for {
		fetches := c.client.PollFetches(ctx)
		if fetches.IsClientClosed() || ctx.Err() != nil {
			return nil
		}
		var fetchErr error
		fetches.EachError(func(topic string, partition int32, err error) {
			if fetchErr == nil {
				fetchErr = fmt.Errorf("kafka: fetch %s/%d: %w", topic, partition, err)
			}
		})
		if fetchErr != nil {
			return fetchErr
		}

		var done []*kgo.Record
		var handleErr error
		fetches.EachRecord(func(rec *kgo.Record) {
			if handleErr != nil {
				return
			}
			msg, err := decode(rec)
			if err != nil {
				if c.onInvalid == nil {
					handleErr = fmt.Errorf("kafka: %s/%d@%d is not a HOTL event: %w", rec.Topic, rec.Partition, rec.Offset, err)
					return
				}
				if err := c.onInvalid(rec, err); err != nil {
					handleErr = err
					return
				}
				done = append(done, rec)
				return
			}
			if err := h(ctx, msg); err != nil {
				handleErr = err
				return
			}
			done = append(done, rec)
		})
		if len(done) > 0 {
			// Commit what was handled even when a later record failed, so it is not handled twice.
			if err := c.client.CommitRecords(context.WithoutCancel(ctx), done...); err != nil && handleErr == nil {
				return fmt.Errorf("kafka: commit: %w", err)
			}
		}
		if handleErr != nil {
			return handleErr
		}
	}
}

// Close leaves the consumer group and closes the connection.
func (c *Consumer) Close() { c.client.Close() }

// decode reads a record as a HOTL envelope, without validating it against the schemas and without requiring its
// type to be known.
func decode(rec *kgo.Record) (Message, error) {
	var env events.Envelope
	if err := json.Unmarshal(rec.Value, &env); err != nil {
		return Message{}, err
	}
	if env.ID == "" || env.Type == "" {
		return Message{}, errors.New("no id or type")
	}
	return Message{Envelope: env, Topic: rec.Topic, Partition: rec.Partition, Offset: rec.Offset}, nil
}
