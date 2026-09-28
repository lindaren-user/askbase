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

type trackingTransactions struct {
	active bool
}

func (m *trackingTransactions) Within(ctx context.Context, fn func(context.Context) error) error {
	m.active = true
	defer func() { m.active = false }()
	return fn(ctx)
}

type fakeStore struct {
	event model.Outbox
	found bool
}

func (s *fakeStore) LockNextDue(
	_ context.Context,
	scheduledStatus string,
	scheduledBefore time.Time,
	leasedStatus string,
	leaseBefore time.Time,
) (model.Outbox, bool, error) {
	if !s.found {
		return model.Outbox{}, false, nil
	}
	if s.event.Status == scheduledStatus && !s.event.NextAttemptAt.After(scheduledBefore) {
		return s.event, true, nil
	}
	if s.event.Status == leasedStatus && s.event.LeaseUntil != nil && !s.event.LeaseUntil.After(leaseBefore) {
		return s.event, true, nil
	}
	return model.Outbox{}, false, nil
}

func (s *fakeStore) UpdateFields(
	_ context.Context,
	id int64,
	conditions map[string]any,
	fields map[string]any,
) (bool, error) {
	if s.event.ID != id {
		return false, nil
	}
	if expected, ok := conditions["status"].(string); ok && s.event.Status != expected {
		return false, nil
	}
	if expected, ok := conditions["lease_token"].(string); ok {
		if s.event.LeaseToken == nil || *s.event.LeaseToken != expected {
			return false, nil
		}
	}
	if attempts, ok := fields["publish_attempts"].(int); ok {
		s.event.PublishAttempts = attempts
	}
	if token, present := fields["lease_token"]; present {
		if token == nil {
			s.event.LeaseToken = nil
		} else if value, ok := token.(string); ok {
			s.event.LeaseToken = &value
		}
	}
	if leaseUntil, present := fields["lease_until"]; present {
		if leaseUntil == nil {
			s.event.LeaseUntil = nil
		} else if value, ok := leaseUntil.(time.Time); ok {
			s.event.LeaseUntil = &value
		}
	}
	if nextAttemptAt, ok := fields["next_attempt_at"].(time.Time); ok {
		s.event.NextAttemptAt = nextAttemptAt
	}
	if lastError, present := fields["last_error"]; present {
		if lastError == nil {
			s.event.LastError = nil
		} else if value, ok := lastError.(string); ok {
			s.event.LastError = &value
		}
	}
	if status, ok := fields["status"].(string); ok {
		s.event.Status = status
	}
	return true, nil
}

func (*fakeStore) DeletePublishedBefore(context.Context, string, time.Time) error {
	return nil
}

type fakePublisher struct {
	err       error
	onPublish func()
}

func (p fakePublisher) PublishOutbox(context.Context, model.Outbox) error {
	if p.onPublish != nil {
		p.onPublish()
	}
	return p.err
}

func TestProcessNextMarksPublished(t *testing.T) {
	store := &fakeStore{event: model.Outbox{ID: 1, Status: model.OutboxStatusPending}, found: true}
	relay := NewRelay(fakeTransactions{}, store, fakePublisher{}, config.OutboxConfig{})

	processed, publishErr, err := relay.processNext(context.Background())
	if err != nil || publishErr != nil {
		t.Fatalf("process next: publishErr=%v err=%v", publishErr, err)
	}
	if !processed || store.event.Status != model.OutboxStatusPublished {
		t.Fatalf("processed=%v status=%q", processed, store.event.Status)
	}
}

func TestProcessNextPublishesOutsideTransaction(t *testing.T) {
	transactions := &trackingTransactions{}
	store := &fakeStore{event: model.Outbox{ID: 1, Status: model.OutboxStatusPending}, found: true}
	publishedOutsideTransaction := false
	publisher := fakePublisher{onPublish: func() {
		publishedOutsideTransaction = !transactions.active
	}}
	relay := NewRelay(transactions, store, publisher, config.OutboxConfig{LeaseDurationMs: 30000})

	processed, publishErr, err := relay.processNext(context.Background())
	if !processed || publishErr != nil || err != nil {
		t.Fatalf("publish outside transaction: processed=%v publishErr=%v err=%v", processed, publishErr, err)
	}
	if !publishedOutsideTransaction {
		t.Fatal("RabbitMQ 发布期间不能持有数据库事务")
	}
}

func TestProcessNextRecordsPublishFailure(t *testing.T) {
	store := &fakeStore{event: model.Outbox{ID: 1, Status: model.OutboxStatusPending, PublishAttempts: 2}, found: true}
	publishErr := errors.New(" broker unavailable ")
	relay := NewRelay(fakeTransactions{}, store, fakePublisher{err: publishErr}, config.OutboxConfig{MaxBackoffMs: 60000})

	processed, gotPublishErr, err := relay.processNext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !processed || !errors.Is(gotPublishErr, publishErr) {
		t.Fatalf("processed=%v publishErr=%v", processed, gotPublishErr)
	}
	if store.event.PublishAttempts != 3 {
		t.Fatalf("attempts=%d", store.event.PublishAttempts)
	}
	if store.event.LastError == nil || *store.event.LastError != "broker unavailable" {
		t.Fatalf("lastError=%v", store.event.LastError)
	}
	delay := time.Until(store.event.NextAttemptAt)
	if delay < 3*time.Second || delay > 5*time.Second {
		t.Fatalf("unexpected retry delay: %s", delay)
	}
}

func TestRunExitsAfterPublishFailure(t *testing.T) {
	store := &fakeStore{event: model.Outbox{ID: 1, Status: model.OutboxStatusPending}, found: true}
	publishErr := errors.New("rabbitmq connection closed")
	startedAt := time.Now()
	relay := NewRelay(
		fakeTransactions{},
		store,
		fakePublisher{err: publishErr},
		config.OutboxConfig{PollIntervalMs: 1000, LeaseDurationMs: 30000, MaxBackoffMs: 60000},
	)

	if err := relay.Run(context.Background()); !errors.Is(err, publishErr) {
		t.Fatalf("发布失败后应退出以重建连接: %v", err)
	}
	if store.event.Status != model.OutboxStatusPending || !store.event.NextAttemptAt.After(startedAt) {
		t.Fatalf("退出前应安排重试: status=%q nextAttemptAt=%s", store.event.Status, store.event.NextAttemptAt)
	}
}

func TestProcessNextReturnsIdle(t *testing.T) {
	relay := NewRelay(fakeTransactions{}, &fakeStore{}, fakePublisher{}, config.OutboxConfig{})
	processed, publishErr, err := relay.processNext(context.Background())
	if err != nil || publishErr != nil || processed {
		t.Fatalf("processed=%v publishErr=%v err=%v", processed, publishErr, err)
	}
}

func TestProcessNextReclaimsExpiredLease(t *testing.T) {
	expiredAt := time.Now().Add(-time.Second)
	oldToken := "old-claim"
	store := &fakeStore{
		event: model.Outbox{
			ID:              1,
			Status:          model.OutboxStatusProcessing,
			PublishAttempts: 1,
			LeaseToken:      &oldToken,
			LeaseUntil:      &expiredAt,
		},
		found: true,
	}
	relay := NewRelay(fakeTransactions{}, store, fakePublisher{}, config.OutboxConfig{LeaseDurationMs: 30000})

	processed, publishErr, err := relay.processNext(context.Background())
	if err != nil || publishErr != nil || !processed {
		t.Fatalf("reclaim expired lease: processed=%v publishErr=%v err=%v", processed, publishErr, err)
	}
	if store.event.Status != model.OutboxStatusPublished || store.event.PublishAttempts != 2 {
		t.Fatalf("status=%q attempts=%d", store.event.Status, store.event.PublishAttempts)
	}
	if store.event.LeaseToken != nil || store.event.LeaseUntil != nil {
		t.Fatal("发布完成后应清除领取租约")
	}
}

func TestProcessNextSkipsActiveLease(t *testing.T) {
	leaseUntil := time.Now().Add(time.Minute)
	store := &fakeStore{
		event: model.Outbox{
			ID:         1,
			Status:     model.OutboxStatusProcessing,
			LeaseUntil: &leaseUntil,
		},
		found: true,
	}
	relay := NewRelay(fakeTransactions{}, store, fakePublisher{}, config.OutboxConfig{LeaseDurationMs: 30000})

	processed, publishErr, err := relay.processNext(context.Background())
	if processed || publishErr != nil || err != nil {
		t.Fatalf("active lease: processed=%v publishErr=%v err=%v", processed, publishErr, err)
	}
}

func TestProcessNextStaleClaimCannotOverwriteNewClaim(t *testing.T) {
	store := &fakeStore{event: model.Outbox{ID: 1, Status: model.OutboxStatusPending}, found: true}
	publisher := fakePublisher{onPublish: func() {
		newToken := "new-claim"
		newLeaseUntil := time.Now().Add(time.Minute)
		store.event.LeaseToken = &newToken
		store.event.LeaseUntil = &newLeaseUntil
	}}
	relay := NewRelay(fakeTransactions{}, store, publisher, config.OutboxConfig{LeaseDurationMs: 30000})

	processed, publishErr, err := relay.processNext(context.Background())
	if !processed || publishErr != nil || err == nil {
		t.Fatalf("stale claim: processed=%v publishErr=%v err=%v", processed, publishErr, err)
	}
	if store.event.Status != model.OutboxStatusProcessing {
		t.Fatalf("新领取状态被覆盖: status=%q", store.event.Status)
	}
	if store.event.LeaseToken == nil || *store.event.LeaseToken != "new-claim" {
		t.Fatalf("新领取结果被覆盖: status=%q token=%v", store.event.Status, store.event.LeaseToken)
	}
}
