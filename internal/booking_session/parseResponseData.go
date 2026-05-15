package booking_session

import (
	"time"

	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type BookingSessionResponse struct {
	ID            uuid.UUID  `json:"id"`
	BookingID     uuid.UUID  `json:"booking_id"`
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
	if response.TrainerJoined.Valid {
		result.TrainerJoined = &response.TrainerJoined.Bool
	}
	if response.ClientJoined.Valid {
		result.ClientJoined = &response.ClientJoined.Bool
	}
	if response.Status.Valid {
		result.Status = &response.Status.String
	}
	if response.TrainerNotes.Valid {
		result.TrainerNotes = &response.TrainerNotes.String
	}
	return *result
}
