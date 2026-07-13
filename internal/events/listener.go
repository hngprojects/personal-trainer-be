package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

func StartPGListener(connStr string, broker *AvailabilityBroker, logger *slog.Logger) (func(), error) {
	l := pq.NewListener(connStr, 5*time.Second, time.Minute, func(ev pq.ListenerEventType, err error) {
		if err != nil {
			logger.Error("pg listener error", "err", err)
		}
	})

	if err := l.Listen("trainer_bookings_events"); err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		for {
			select {
			case n, ok := <-l.Notify:
				if !ok {
					return
				}
				if n == nil {
					continue
				}
				var row struct {
					TrainerID uuid.UUID `json:"trainer_id"`
					StartTime time.Time `json:"scheduled_start"`
				}
				if err := json.Unmarshal([]byte(n.Extra), &row); err != nil {
					logger.Error("bad notify payload", "err", err)
					continue
				}
				event := struct {
					TrainerID uuid.UUID `json:"trainer_id"`
					StartTime time.Time `json:"scheduled_start"`
				}{
					TrainerID: row.TrainerID,
					StartTime: row.StartTime,
				}
				safePayload, err := json.Marshal(event)
				if err != nil {
					logger.Error("failed to marshal availability event", "err", err)
					continue
				}
				broker.Publish(row.TrainerID, string(safePayload))

			case <-ctx.Done():
				if err := l.Close(); err != nil {
					logger.Error("failed to close pg listener", "err", err)
				}
				return
			}
		}
	}()

	return cancel, nil
}
