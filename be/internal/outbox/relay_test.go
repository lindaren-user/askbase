package outbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"askbase/be/internal/config"
	"askbase/be/internal/model"
)

type fakeTransactions struct{}

func (fakeTransactions) Within(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type fakeStore struct {
	event            model.Outbox
	found            bool
	publishedStatus  string
	failureAttempts  int
	failureNextAt    time.Time
	failureLastError string
}

func (s *fakeStore) LockNextByStatus(context.Context, string, time.Time) (model.Outbox, bool, error) {
	return s.event, s.found, nil
}

func (s *fakeStore) UpdatePublishFailure(_ context.Context, _ int64, attempts int, nextAttemptAt time.Time, lastError string, _ time.Time) error {
	s.failureAttempts = attempts
	s.failureNextAt = nextAttemptAt
	s.failureLastError = lastError
	return nil
}

func (s *fakeStore) UpdatePublished(_ context.Context, _ int64, status string, _ time.Time) error {
	s.publishedStatus = status
	return nil
}

func (*fakeStore) DeletePublishedBefore(context.Context, string, time.Time) error {
	return nil
}

type fakePublisher struct {
	err error
}

func (p fakePublisher) PublishOutbox(context.Context, model.Outbox) error {
	return p.err
}

func TestProcessNextMarksPublished(t *testing.T) {
	store := &fakeStore{event: model.Outbox{ID: 1}, found: true}
	relay := NewRelay(fakeTransactions{}, store, fakePublisher{}, config.OutboxConfig{})

	processed, publishErr, err := relay.processNext(context.Background())
	if err != nil || publishErr != nil {
		t.Fatalf("process next: publishErr=%v err=%v", publishErr, err)
	}
	if !processed || store.publishedStatus != model.OutboxStatusPublished {
		t.Fatalf("processed=%v status=%q", processed, store.publishedStatus)
	}
}

func TestProcessNextRecordsPublishFailure(t *testing.T) {
	store := &fakeStore{event: model.Outbox{ID: 1, PublishAttempts: 2}, found: true}
	publishErr := errors.New(" broker unavailable ")
	relay := NewRelay(fakeTransactions{}, store, fakePublisher{err: publishErr}, config.OutboxConfig{MaxBackoffMs: 60000})

	processed, gotPublishErr, err := relay.processNext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !processed || !errors.Is(gotPublishErr, publishErr) {
		t.Fatalf("processed=%v publishErr=%v", processed, gotPublishErr)
	}
	if store.failureAttempts != 3 || store.failureLastError != "broker unavailable" {
		t.Fatalf("attempts=%d lastError=%q", store.failureAttempts, store.failureLastError)
	}
	delay := time.Until(store.failureNextAt)
	if delay < 3*time.Second || delay > 5*time.Second {
		t.Fatalf("unexpected retry delay: %s", delay)
	}
}

func TestProcessNextReturnsIdle(t *testing.T) {
	relay := NewRelay(fakeTransactions{}, &fakeStore{}, fakePublisher{}, config.OutboxConfig{})
	processed, publishErr, err := relay.processNext(context.Background())
	if err != nil || publishErr != nil || processed {
		t.Fatalf("processed=%v publishErr=%v err=%v", processed, publishErr, err)
	}
}
