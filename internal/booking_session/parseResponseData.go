package booking_session

import (
	"time"

	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type BookingSessionResponse struct {
	ID            uuid.UUID  `json:"id"`
	BookingID     uuid.UUID  `json:"booking_id"`
	TrainerID     *uuid.UUID `json:"trainer_id,omitempty"`
	ActualStart   *time.Time `json:"actual_start"`
	ActualEnd     *time.Time `json:"actual_end"`
	TrainerJoined *bool      `json:"trainer_joined"`
	ClientJoined  *bool      `json:"client_joined"`
	Status        *string    `json:"status"`
	TrainerNotes  *string    `json:"trainer_notes"`
	CreatedAt     *time.Time `json:"created_at"`
}

func ParseResponse(response *db.BookingSession) BookingSessionResponse {
	result := &BookingSessionResponse{
		ID:        response.ID,
		BookingID: response.BookingID,
		Status:    &response.Status,
		CreatedAt: &response.CreatedAt,
	}
	if response.ActualStart.Valid {
		result.ActualStart = &response.ActualStart.Time
	}
	if response.ActualEnd.Valid {
		result.ActualEnd = &response.ActualEnd.Time
	}
	if response.TrainerJoined.Valid {
		result.TrainerJoined = &response.TrainerJoined.Bool
	}
	if response.ClientJoined.Valid {
		result.ClientJoined = &response.ClientJoined.Bool
	}
	if response.TrainerNotes.Valid {
		result.TrainerNotes = &response.TrainerNotes.String
	}
	return *result
}

// ParseResponseWithTrainer renders a session row that was joined with its
// parent booking, so the trainer_id is included in the response. Used by
// the GET /sessions/{id} handler.
func ParseResponseWithTrainer(response *db.GetBookingSessionByIdRow) BookingSessionResponse {
	trainerID := response.TrainerID
	result := &BookingSessionResponse{
		ID:        response.ID,
		BookingID: response.BookingID,
		TrainerID: &trainerID,
		Status:    &response.Status,
		CreatedAt: &response.CreatedAt,
	}
	if response.ActualStart.Valid {
		result.ActualStart = &response.ActualStart.Time
	}
	if response.ActualEnd.Valid {
		result.ActualEnd = &response.ActualEnd.Time
	}
	if response.TrainerJoined.Valid {
		result.TrainerJoined = &response.TrainerJoined.Bool
	}
	if response.ClientJoined.Valid {
		result.ClientJoined = &response.ClientJoined.Bool
	}
	if response.TrainerNotes.Valid {
		result.TrainerNotes = &response.TrainerNotes.String
	}
	return *result
}
