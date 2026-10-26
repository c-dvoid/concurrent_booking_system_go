package booking

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

// newTestRedis connects to a local Redis, skipping the test if it isn't running.
func newTestRedis(t *testing.T) *goredis.Client {
	t.Helper()

	rdb := goredis.NewClient(&goredis.Options{Addr: "localhost:6379"})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		rdb.Close()
		t.Skipf("redis not available: %v", err)
	}
	t.Cleanup(func() { rdb.Close() })

	return rdb
}

func TestConcurrentBooking_ExactlyOneWins(t *testing.T) {
	rdb := newTestRedis(t)
	svc := NewService(NewRedisStore(rdb))

	const numGoroutines = 1_000 // users trying to book the same seat at the same time

	// A unique movie per run so leftover holds from earlier runs don't interfere.
	movieID := "test-" + uuid.NewString()
	seatID := "A1"
	ctx := context.Background()

	var (
		successes atomic.Int64
		failures  atomic.Int64
		winner    atomic.Value
		wg        sync.WaitGroup
	)

	t.Cleanup(func() {
		keys := []string{seatKey(movieID, seatID)}
		if id, ok := winner.Load().(string); ok {
			keys = append(keys, sessionKey(id))
		}
		rdb.Del(context.Background(), keys...)
	})

	wg.Add(numGoroutines)
	for range numGoroutines {
		go func() {
			defer wg.Done()
			session, err := svc.Book(ctx, Booking{
				MovieID: movieID,
				SeatID:  seatID,
				UserID:  uuid.NewString(),
			})
			switch {
			case err == nil:
				successes.Add(1)
				winner.Store(session.ID)
			case errors.Is(err, ErrSeatAlreadyBooked):
				failures.Add(1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := successes.Load(); got != 1 {
		t.Errorf("expected exactly 1 success, got %d", got)
	}
	if got := failures.Load(); got != int64(numGoroutines-1) {
		t.Errorf("expected %d failures, got %d", numGoroutines-1, got)
	}
}
