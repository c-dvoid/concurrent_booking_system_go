package booking

import (
	"context"
	"errors"
	"time"
)

var (
	ErrSeatAlreadyBooked = errors.New("this seat is already taken")
	ErrSessionNotFound   = errors.New("session not found or expired")
	ErrNotSessionOwner   = errors.New("session belongs to another user")
	ErrAlreadyConfirmed  = errors.New("booking is already confirmed")
)

type Booking struct {
	ID        string
	MovieID   string
	SeatID    string
	UserID    string
	Status    string
	ExpiresAt time.Time
}

type BookingStore interface {
	Book(ctx context.Context, b Booking) (Booking, error)
	ListBookings(ctx context.Context, movieID string) ([]Booking, error)

	Confirm(ctx context.Context, sessionID string, userID string) (Booking, error)
	Release(ctx context.Context, sessionID string, userID string) error
}
