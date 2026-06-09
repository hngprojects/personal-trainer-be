package events

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

func StartPGListener(connStr string, broker *AvailabilityBroker, logger *slog.Logger) error {
	l := pq.NewListener(connStr, 5*time.Second, time.Minute, func(ev pq.ListenerEventType, err error) {
		if err != nil {
			logger.Error("pg listener error", "err", err)
		}
	})

	if err := l.Listen("trainer_bookings_events"); err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	go func() {
		for n := range l.Notify {
			if n == nil {
				continue
			}
			var row struct {
				TrainerID uuid.UUID `json:"trainer_id"`
			}
			if err := json.Unmarshal([]byte(n.Extra), &row); err != nil {
				logger.Error("bad notify payload", "err", err)
				continue
			}
			broker.Publish(row.TrainerID, n.Extra)
		}
	}()

	return nil
}
