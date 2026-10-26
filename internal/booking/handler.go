package booking

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/c-dvoid/concurrent_booking_system_go/internal/utils"
)

type handler struct {
	svc *Service
}

func NewHandler(svc *Service) *handler {
	return &handler{svc}
}

type holdSeatRequest struct {
	UserID string `json:"user_id"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, status int, msg string) {
	utils.WriteJSON(w, status, errorResponse{Error: msg})
}

// writeStoreError maps store errors to HTTP statuses.
func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrSeatAlreadyBooked), errors.Is(err, ErrAlreadyConfirmed):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrSessionNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrNotSessionOwner):
		writeError(w, http.StatusForbidden, err.Error())
	default:
		log.Println(err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// decodeUser reads the request body and requires a non-empty user_id.
func decodeUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req holdSeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return "", false
	}
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return "", false
	}
	return req.UserID, true
}

func (h *handler) HoldSeat(w http.ResponseWriter, r *http.Request) {
	movieID := r.PathValue("movieID")
	seatID := r.PathValue("seatID")

	userID, ok := decodeUser(w, r)
	if !ok {
		return
	}

	data := Booking{
		UserID:  userID,
		SeatID:  seatID,
		MovieID: movieID,
	}

	session, err := h.svc.Book(r.Context(), data)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	type holdResponse struct {
		SessionID string `json:"session_id"`
		MovieID   string `json:"movie_id"`
		SeatID    string `json:"seat_id"`
		ExpiresAt string `json:"expires_at"`
	}

	utils.WriteJSON(w, http.StatusCreated, holdResponse{
		SeatID:    session.SeatID,
		MovieID:   session.MovieID,
		SessionID: session.ID,
		ExpiresAt: session.ExpiresAt.Format(time.RFC3339),
	})
}

func (h *handler) ListSeats(w http.ResponseWriter, r *http.Request) {
	movieID := r.PathValue("movieID")

	bookings, err := h.svc.ListBookings(r.Context(), movieID)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	seats := make([]seatInfo, 0, len(bookings))
	for _, b := range bookings {
		seats = append(seats, seatInfo{
			SeatID:    b.SeatID,
			UserID:    b.UserID,
			Booked:    true,
			Confirmed: b.Status == "confirmed",
		})
	}

	utils.WriteJSON(w, http.StatusOK, seats)
}

type seatInfo struct {
	SeatID    string `json:"seat_id"`
	UserID    string `json:"user_id"`
	Booked    bool   `json:"booked"`
	Confirmed bool   `json:"confirmed"`
}

func (h *handler) ConfirmSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")

	userID, ok := decodeUser(w, r)
	if !ok {
		return
	}

	session, err := h.svc.ConfirmSeat(r.Context(), sessionID, userID)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	utils.WriteJSON(w, http.StatusOK, sessionResponse{
		SessionID: session.ID,
		MovieID:   session.MovieID,
		SeatID:    session.SeatID,
		UserID:    session.UserID,
		Status:    session.Status,
	})
}

type sessionResponse struct {
	SessionID string `json:"session_id"`
	MovieID   string `json:"movie_id"`
	SeatID    string `json:"seat_id"`
	UserID    string `json:"user_id"`
	Status    string `json:"status"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

func (h *handler) ReleaseSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")

	userID, ok := decodeUser(w, r)
	if !ok {
		return
	}

	if err := h.svc.ReleaseSeat(r.Context(), sessionID, userID); err != nil {
		writeStoreError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
