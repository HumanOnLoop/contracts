package kafka

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/HumanOnLoop/contracts/events"
)

// TestIntegrationProduceConsume publishes events and reads them back through a real Kafka. It runs only when
// HOTL_KAFKA_BROKERS is set (make test-kafka sets it for the k3d-hotl cluster). It uses temporary topics under a
// random prefix and deletes them afterwards, so it never writes to the real hotl.* topics.
func TestIntegrationProduceConsume(t *testing.T) {
	brokers := os.Getenv("HOTL_KAFKA_BROKERS")
	if brokers == "" {
		t.Skip("HOTL_KAFKA_BROKERS not set")
	}
	seeds := strings.Split(brokers, ",")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	b := make([]byte, 4)
	_, _ = rand.Read(b)
	prefix := "test-" + hex.EncodeToString(b) + "."
	admin, err := kgo.NewClient(kgo.SeedBrokers(seeds...))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	topics := []string{prefix + events.TopicRun, prefix + events.TopicBrief}
	createTopics(ctx, t, admin, topics)
	defer deleteTopics(t, admin, topics)

	p, err := NewProducer(seeds, WithTopicPrefix(prefix))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	run := runStarted(t, "RUN-0042-1")
	paused, err := events.New(events.RunPaused{Reason: events.RunPausedReasonBlocked, BriefIDs: []events.BriefID{"BRF-0107"}},
		events.Meta{Source: "/hotl/test", Subject: "RUN-0042-1", RunID: "RUN-0042-1"})
	if err != nil {
		t.Fatal(err)
	}
	withdrawn, err := events.New(events.BriefWithdrawn{WithdrawnBy: events.AgentRoleImplementer, Reason: "No longer needed."},
		events.Meta{Source: "/hotl/test", Subject: "BRF-0107", RunID: "RUN-0042-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Publish(ctx, run, paused, withdrawn); err != nil {
		t.Fatalf("publish: %v", err)
	}

	// An invalid event is refused before anything is sent.
	bad := run
	bad.Subject = ""
	if err := p.Publish(ctx, bad); err == nil {
		t.Error("published an invalid event")
	}

	group := prefix + "group"
	defer deleteGroup(t, admin, group)
	got := consume(ctx, t, seeds, group, prefix, 3)
	if got[0].ID != run.ID || got[1].ID != paused.ID {
		t.Errorf("run events out of order: %s, %s", got[0].Type, got[1].Type)
	}
	if got[2].ID != withdrawn.ID || !strings.HasSuffix(got[2].Topic, events.TopicBrief) {
		t.Errorf("brief event = %+v", got[2])
	}
	rs, err := events.As[events.RunStarted](got[0].Envelope)
	if err != nil || rs.Developer != "Developer, Orders team" {
		t.Errorf("RunStarted = %+v, %v", rs, err)
	}

	// Offsets were committed: the same group sees nothing new.
	publishOne(ctx, t, p)
	more := consume(ctx, t, seeds, group, prefix, 1)
	if more[0].Type != "RunResumed" {
		t.Errorf("after commit, got %s first; want only the new RunResumed", more[0].Type)
	}
}

func publishOne(ctx context.Context, t *testing.T, p *Producer) {
	t.Helper()
	env, err := events.New(events.RunResumed{}, events.Meta{Source: "/hotl/test", Subject: "RUN-0042-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Publish(ctx, env); err != nil {
		t.Fatal(err)
	}
}

// consume runs a consumer until it has n messages, ordering run events before brief events.
func consume(ctx context.Context, t *testing.T, seeds []string, group, prefix string, n int) []Message {
	t.Helper()
	c, err := NewConsumer(seeds, group, []string{events.TopicRun, events.TopicBrief}, WithTopicPrefix(prefix))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var run, brief []Message
	stop := errors.New("enough")
	err = c.Run(ctx, func(_ context.Context, m Message) error {
		if strings.HasSuffix(m.Topic, events.TopicRun) {
			run = append(run, m)
		} else {
			brief = append(brief, m)
		}
		if len(run)+len(brief) == n {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) {
		t.Fatalf("consumer stopped with %v after %d messages", err, len(run)+len(brief))
	}
	return append(run, brief...)
}

func createTopics(ctx context.Context, t *testing.T, cl *kgo.Client, names []string) {
	t.Helper()
	req := kmsg.NewPtrCreateTopicsRequest()
	req.TimeoutMillis = 30000
	for _, n := range names {
		rt := kmsg.NewCreateTopicsRequestTopic()
		rt.Topic, rt.NumPartitions, rt.ReplicationFactor = n, 3, 1
		req.Topics = append(req.Topics, rt)
	}
	resp, err := req.RequestWith(ctx, cl)
	if err != nil {
		t.Fatal(err)
	}
	for _, rt := range resp.Topics {
		if rt.ErrorCode != 0 {
			t.Fatalf("create topic %s: error code %d", rt.Topic, rt.ErrorCode)
		}
	}
}

func deleteTopics(t *testing.T, cl *kgo.Client, names []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req := kmsg.NewPtrDeleteTopicsRequest()
	req.TimeoutMillis = 30000
	req.TopicNames = names
	for _, n := range names {
		rt := kmsg.NewDeleteTopicsRequestTopic()
		rt.Topic = kmsg.StringPtr(n)
		req.Topics = append(req.Topics, rt)
	}
	if _, err := req.RequestWith(ctx, cl); err != nil {
		t.Errorf("delete test topics %v: %v", names, err)
	}
}

func deleteGroup(t *testing.T, cl *kgo.Client, group string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req := kmsg.NewPtrDeleteGroupsRequest()
	req.Groups = []string{group}
	resp, err := req.RequestWith(ctx, cl)
	if err != nil {
		t.Errorf("delete test group %s: %v", group, err)
		return
	}
	for _, g := range resp.Groups {
		if g.ErrorCode != 0 {
			t.Errorf("delete test group %s: error code %d", g.Group, g.ErrorCode)
		}
	}
}
