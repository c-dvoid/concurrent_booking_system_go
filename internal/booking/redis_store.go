package booking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const defaultHoldTTL = 2 * time.Minute

// loadSessionLua resolves a session to its seat booking and checks that the
// seat still belongs to that session and user. Shared by the scripts below so
// the check and the write happen atomically inside Redis.
// KEYS[1] = session key, ARGV[1] = session ID, ARGV[2] = user ID.
const loadSessionLua = `
local sk = redis.call('GET', KEYS[1])
if not sk then return redis.error_reply('NOT_FOUND') end
local val = redis.call('GET', sk)
if not val then return redis.error_reply('NOT_FOUND') end
local b = cjson.decode(val)
if b.ID ~= ARGV[1] then return redis.error_reply('NOT_FOUND') end
if b.UserID ~= ARGV[2] then return redis.error_reply('FORBIDDEN') end
`

// confirmScript marks the booking confirmed and drops the TTL on both keys.
var confirmScript = redis.NewScript(loadSessionLua + `
b.Status = 'confirmed'
b.ExpiresAt = nil
local out = cjson.encode(b)
redis.call('SET', sk, out)
redis.call('PERSIST', KEYS[1])
return out
`)

// releaseScript deletes a held (not yet confirmed) booking.
var releaseScript = redis.NewScript(loadSessionLua + `
if b.Status == 'confirmed' then return redis.error_reply('CONFIRMED') end
redis.call('DEL', sk, KEYS[1])
return 1
`)

type RedisStore struct {
	rdb *redis.Client
}

func NewRedisStore(rdb *redis.Client) *RedisStore {
	return &RedisStore{rdb: rdb}
}

// sessionKey builds the reverse-lookup key for a session.
func sessionKey(id string) string {
	return fmt.Sprintf("session:%s", id)
}

func seatKey(movieID, seatID string) string {
	return fmt.Sprintf("seat:%s:%s", movieID, seatID)
}

func (s *RedisStore) Book(ctx context.Context, b Booking) (Booking, error) {
	session, err := s.hold(ctx, b)
	if err != nil {
		return Booking{}, err
	}

	log.Printf("Session booked %v", session)

	return session, nil
}

func (s *RedisStore) ListBookings(ctx context.Context, movieID string) ([]Booking, error) {
	pattern := seatKey(movieID, "*")
	var sessions []Booking

	iter := s.rdb.Scan(ctx, 0, pattern, 0).Iterator()
	for iter.Next(ctx) {
		val, err := s.rdb.Get(ctx, iter.Val()).Result()
		if err != nil {
			// The hold may have expired between SCAN and GET.
			continue
		}
		session, err := parseSession(val)
		if err != nil {
			continue
		}
		sessions = append(sessions, session)
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}

	return sessions, nil
}

func (s *RedisStore) hold(ctx context.Context, b Booking) (Booking, error) {
	key := seatKey(b.MovieID, b.SeatID)

	b.ID = uuid.New().String()
	b.Status = "held"
	b.ExpiresAt = time.Now().Add(defaultHoldTTL)

	val, err := json.Marshal(b)
	if err != nil {
		return Booking{}, err
	}

	err = s.rdb.SetArgs(ctx, key, val, redis.SetArgs{
		Mode: "NX", // set if not exists
		TTL:  defaultHoldTTL,
	}).Err()
	if errors.Is(err, redis.Nil) {
		return Booking{}, ErrSeatAlreadyBooked
	}
	if err != nil {
		return Booking{}, err
	}

	if err := s.rdb.Set(ctx, sessionKey(b.ID), key, defaultHoldTTL).Err(); err != nil {
		// Without the session key the hold can't be confirmed or released, so undo it.
		s.rdb.Del(ctx, key)
		return Booking{}, err
	}

	return b, nil
}

func parseSession(val string) (Booking, error) {
	var data Booking
	if err := json.Unmarshal([]byte(val), &data); err != nil {
		return Booking{}, err
	}
	return data, nil
}

// Confirm converts a held session into a permanent booking.
// Removes the TTL so the keys never expire.
func (s *RedisStore) Confirm(ctx context.Context, sessionID string, userID string) (Booking, error) {
	val, err := confirmScript.Run(ctx, s.rdb, []string{sessionKey(sessionID)}, sessionID, userID).Text()
	if err != nil {
		return Booking{}, mapScriptErr(err)
	}

	return parseSession(val)
}

func (s *RedisStore) Release(ctx context.Context, sessionID string, userID string) error {
	err := releaseScript.Run(ctx, s.rdb, []string{sessionKey(sessionID)}, sessionID, userID).Err()
	return mapScriptErr(err)
}

func mapScriptErr(err error) error {
	if err == nil {
		return nil
	}
	switch msg := err.Error(); {
	case strings.HasPrefix(msg, "NOT_FOUND"):
		return ErrSessionNotFound
	case strings.HasPrefix(msg, "FORBIDDEN"):
		return ErrNotSessionOwner
	case strings.HasPrefix(msg, "CONFIRMED"):
		return ErrAlreadyConfirmed
	}
	return err
}
