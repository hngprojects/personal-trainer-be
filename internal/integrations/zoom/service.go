package zoom

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/meeting"
)

type Service struct {
	q       *db.Queries
	meeting meeting.Provider
	log     *slog.Logger
}

func NewService(q *db.Queries, meetingProvider meeting.Provider, log *slog.Logger) *Service {
	return &Service{q: q, meeting: meetingProvider, log: log}
}

type MeetingResult struct {
	JoinURL   string
	MeetingID string
	Passcode  string
	Existing  bool // true if the meeting was already created (idempotent hit)
}

var (
	ErrNotFound      = errors.New("booking not found")
	ErrNotConfigured   = errors.New("zoom not configured")
	ErrNoScheduledStart = errors.New("booking has no scheduled start time")
)

// EnsureDiscoveryMeeting creates a Zoom meeting for a discovery booking or returns
// the existing one if already present (idempotency).
func (s *Service) EnsureDiscoveryMeeting(ctx context.Context, bookingID uuid.UUID) (*MeetingResult, error) {
	booking, err := s.q.GetDiscoveryBookingByID(ctx, bookingID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get discovery booking: %w", err)
	}

	// Idempotency: return existing meeting if present
	if booking.ZoomMeetingID.Valid && booking.ZoomMeetingID.String != "" {
		s.log.Info("zoom meeting already exists for discovery booking",
			"booking_id", bookingID,
			"meeting_id", booking.ZoomMeetingID.String,
		)
		return &MeetingResult{
			JoinURL:   booking.ZoomMeetingLink.String,
			MeetingID: booking.ZoomMeetingID.String,
			Passcode:  booking.ZoomPasscode.String,
			Existing:  true,
		}, nil
	}

	if !s.meeting.IsConfigured() {
		return nil, ErrNotConfigured
	}

	topic := "FitCall Discovery Call"
	link, meetingID, passcode, err := s.meeting.CreateMeeting(ctx, topic, booking.SelectedDatetime, 30)
	if err != nil {
		s.log.Error("failed to create zoom meeting for discovery booking",
			"booking_id", bookingID,
			"err", err,
		)
		return nil, fmt.Errorf("create zoom meeting: %w", err)
	}

	if _, dbErr := s.q.UpdateDiscoveryBookingZoom(ctx, db.UpdateDiscoveryBookingZoomParams{
		ID:              bookingID,
		ZoomMeetingLink: sql.NullString{String: link, Valid: true},
		ZoomMeetingID:   sql.NullString{String: meetingID, Valid: true},
		ZoomPasscode:    sql.NullString{String: passcode, Valid: passcode != ""},
	}); dbErr != nil {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanCancel()

		if errors.Is(dbErr, sql.ErrNoRows) {
			// Another concurrent request won the write race (zoom_meeting_id IS NULL check failed).
			// Our newly-created meeting is orphaned — delete it and return the winner's data.
			if delErr := s.meeting.DeleteMeeting(cleanCtx, meetingID); delErr != nil {
				s.log.Warn("orphaned zoom meeting from lost write race — manual cleanup required",
					"meeting_id", meetingID,
					"err", delErr,
				)
			}
			winner, fetchErr := s.q.GetDiscoveryBookingByID(ctx, bookingID)
			if fetchErr == nil && winner.ZoomMeetingID.Valid {
				return &MeetingResult{
					JoinURL:   winner.ZoomMeetingLink.String,
					MeetingID: winner.ZoomMeetingID.String,
					Passcode:  winner.ZoomPasscode.String,
					Existing:  true,
				}, nil
			}
			return nil, fmt.Errorf("persist zoom meeting: %w", dbErr)
		}

		// General DB failure — meeting created but not persisted; attempt cleanup.
		s.log.Error("failed to persist zoom meeting for discovery booking — attempting cleanup",
			"booking_id", bookingID,
			"meeting_id", meetingID,
			"err", dbErr,
		)
		if delErr := s.meeting.DeleteMeeting(cleanCtx, meetingID); delErr != nil {
			s.log.Error("orphaned zoom meeting — manual cleanup required",
				"meeting_id", meetingID,
				"err", delErr,
			)
		}
		return nil, fmt.Errorf("persist zoom meeting: %w", dbErr)
	}

	s.log.Info("zoom meeting created for discovery booking",
		"booking_id", bookingID,
		"meeting_id", meetingID,
	)
	return &MeetingResult{JoinURL: link, MeetingID: meetingID, Passcode: passcode}, nil
}

// EnsurePaidSessionMeeting creates a Zoom meeting for a paid booking or returns
// the existing one if already present (idempotency).
func (s *Service) EnsurePaidSessionMeeting(ctx context.Context, bookingID uuid.UUID) (*MeetingResult, error) {
	info, err := s.q.GetBookingZoomInfo(ctx, bookingID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get booking zoom info: %w", err)
	}

	// Idempotency: return existing meeting if present
	if info.ZoomMeetingID.Valid && info.ZoomMeetingID.String != "" {
		s.log.Info("zoom meeting already exists for paid booking",
			"booking_id", bookingID,
			"meeting_id", info.ZoomMeetingID.String,
		)
		return &MeetingResult{
			JoinURL:   info.ZoomMeetingLink.String,
			MeetingID: info.ZoomMeetingID.String,
			Passcode:  info.ZoomPasscode.String,
			Existing:  true,
		}, nil
	}

	if !s.meeting.IsConfigured() {
		return nil, ErrNotConfigured
	}

	if !info.ScheduledStart.Valid {
		return nil, ErrNoScheduledStart
	}

	durationMins := 60
	if info.ScheduledEnd.Valid {
		computed := int(info.ScheduledEnd.Time.Sub(info.ScheduledStart.Time).Minutes())
		if computed > 0 {
			durationMins = computed
		}
	}

	topic := "Training Session"
	link, meetingID, passcode, err := s.meeting.CreateMeeting(ctx, topic, info.ScheduledStart.Time, durationMins)
	if err != nil {
		s.log.Error("failed to create zoom meeting for paid booking",
			"booking_id", bookingID,
			"err", err,
		)
		return nil, fmt.Errorf("create zoom meeting: %w", err)
	}

	if _, dbErr := s.q.UpdateBookingZoom(ctx, db.UpdateBookingZoomParams{
		ID:              bookingID,
		ZoomMeetingLink: sql.NullString{String: link, Valid: true},
		ZoomMeetingID:   sql.NullString{String: meetingID, Valid: true},
		ZoomPasscode:    sql.NullString{String: passcode, Valid: passcode != ""},
	}); dbErr != nil {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanCancel()

		if errors.Is(dbErr, sql.ErrNoRows) {
			// Another concurrent request won the write race (zoom_meeting_id IS NULL check failed).
			// Our newly-created meeting is orphaned — delete it and return the winner's data.
			if delErr := s.meeting.DeleteMeeting(cleanCtx, meetingID); delErr != nil {
				s.log.Warn("orphaned zoom meeting from lost write race — manual cleanup required",
					"meeting_id", meetingID,
					"err", delErr,
				)
			}
			winner, fetchErr := s.q.GetBookingZoomInfo(ctx, bookingID)
			if fetchErr == nil && winner.ZoomMeetingID.Valid {
				return &MeetingResult{
					JoinURL:   winner.ZoomMeetingLink.String,
					MeetingID: winner.ZoomMeetingID.String,
					Passcode:  winner.ZoomPasscode.String,
					Existing:  true,
				}, nil
			}
			return nil, fmt.Errorf("persist zoom meeting: %w", dbErr)
		}

		// General DB failure — meeting created but not persisted; attempt cleanup.
		s.log.Error("failed to persist zoom meeting for paid booking — attempting cleanup",
			"booking_id", bookingID,
			"meeting_id", meetingID,
			"err", dbErr,
		)
		if delErr := s.meeting.DeleteMeeting(cleanCtx, meetingID); delErr != nil {
			s.log.Error("orphaned zoom meeting — manual cleanup required",
				"meeting_id", meetingID,
				"err", delErr,
			)
		}
		return nil, fmt.Errorf("persist zoom meeting: %w", dbErr)
	}

	s.log.Info("zoom meeting created for paid booking",
		"booking_id", bookingID,
		"meeting_id", meetingID,
	)
	return &MeetingResult{JoinURL: link, MeetingID: meetingID, Passcode: passcode}, nil
}
