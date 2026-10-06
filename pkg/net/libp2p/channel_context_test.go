package libp2p

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/keep-network/keep-core/pkg/net/gen/pb"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

type blockedPublisher struct{ entered chan struct{} }

func (p blockedPublisher) Publish(ctx context.Context, _ []byte, _ ...pubsub.PubOpt) error {
	select {
	case p.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return ctx.Err()
}
func TestPublicationHonorsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := blockedPublisher{make(chan struct{}, 1)}
	c := &channel{publisher: p}
	done := make(chan error, 1)
	go func() { done <- c.publish(ctx, &pb.BroadcastNetworkMessage{}) }()
	select {
	case <-p.entered:
	case <-time.After(time.Second):
		t.Fatal("publisher not entered")
	}
	cancel()
	select {
	case e := <-done:
		if !errors.Is(e, context.Canceled) {
			t.Fatal("wrong publication result")
		}
	case <-time.After(time.Second):
		t.Fatal("publication ignored cancellation")
	}
}

func TestQueuedPublicationHonorsContext(t *testing.T) {
	first, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()
	p := blockedPublisher{make(chan struct{}, 1)}
	c := &channel{publisher: p}
	done := make(chan error, 1)
	go func() { done <- c.publish(first, &pb.BroadcastNetworkMessage{}) }()
	<-p.entered
	second, cancelSecond := context.WithCancel(context.Background())
	waiting := make(chan error, 1)
	go func() { waiting <- c.publish(second, &pb.BroadcastNetworkMessage{}) }()
	cancelSecond()
	select {
	case e := <-waiting:
		if !errors.Is(e, context.Canceled) {
			t.Fatal("queued send ignored cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("queued publication waited for unrelated send")
	}
	cancelFirst()
	<-done
}
