package uploads

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/storage"
)

// TrainerDisplayPictureJob is the unit of work for the display-picture
// pipeline. Same in-memory shape as AvatarJob — pictures are capped at 5 MiB
// in the handler so holding bytes in the channel is fine; no disk-backed
// dance like the video pipeline.
type TrainerDisplayPictureJob struct {
	TrainerID   uuid.UUID
	ObjectKey   string // bucket-relative path, passed verbatim to Storage.PutObject
	PublicURL   string // what gets UPDATEd into trainers.display_picture on success
	ContentType string
	Bytes       []byte
}

// TrainerDisplayPictureUploader runs the gallery-image-style background
// pipeline for a trainer's *display picture*: PutObject to MinIO → UPDATE
// trainers.display_picture. The trainer row is inserted (with display_picture
// NULL) by the handler before enqueueing, so on success we only flip the URL
// field; on failure the row stays as-is and ops sees a loud log line.
type TrainerDisplayPictureUploader struct {
	store storage.Storage
	q     *db.Queries
	log   *slog.Logger
	jobs  chan TrainerDisplayPictureJob
	wg    sync.WaitGroup

	mu     sync.RWMutex
	closed bool
	stopCh chan struct{}
}

func NewTrainerDisplayPictureUploader(store storage.Storage, q *db.Queries, log *slog.Logger, bufferSize int) *TrainerDisplayPictureUploader {
	return &TrainerDisplayPictureUploader{
		store:  store,
		q:      q,
		log:    log,
		jobs:   make(chan TrainerDisplayPictureJob, bufferSize),
		stopCh: make(chan struct{}),
	}
}

func (u *TrainerDisplayPictureUploader) Start(workers int) {
	for i := 0; i < workers; i++ {
		u.wg.Add(1)
		go u.run(i)
	}
}

// Stop mirrors AvatarUploader.Stop: close stopCh BEFORE wg.Wait so any worker
// parked in retry backoff exits immediately instead of sleeping the full
// 1s/2s window during shutdown.
func (u *TrainerDisplayPictureUploader) Stop() {
	u.mu.Lock()
	if u.closed {
		u.mu.Unlock()
		return
	}
	u.closed = true
	close(u.jobs)
	close(u.stopCh)
	u.mu.Unlock()
	u.wg.Wait()
}

func (u *TrainerDisplayPictureUploader) Enqueue(job TrainerDisplayPictureJob) error {
	u.mu.RLock()
	defer u.mu.RUnlock()
	if u.closed {
		return ErrUploaderClosed
	}
	select {
	case u.jobs <- job:
		return nil
	default:
		return ErrQueueFull
	}
}

func (u *TrainerDisplayPictureUploader) run(workerID int) {
	defer u.wg.Done()
	for job := range u.jobs {
		u.process(workerID, job)
	}
}

const (
	trainerDisplayPictureMaxAttempts    = 3
	trainerDisplayPictureAttemptTimeout = 30 * time.Second
)

func (u *TrainerDisplayPictureUploader) process(workerID int, job TrainerDisplayPictureJob) {
	var lastErr error
	for attempt := 1; attempt <= trainerDisplayPictureMaxAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), trainerDisplayPictureAttemptTimeout)
		err := u.store.PutObject(ctx, job.ObjectKey, bytes.NewReader(job.Bytes), int64(len(job.Bytes)), job.ContentType)
		cancel()

		if err == nil {
			// Storage write succeeded. Flip trainers.display_picture so the
			// URL the handler returned in the 201 starts resolving.
			rows, dbErr := u.updateDisplayPicture(job)
			switch {
			case dbErr != nil:
				u.log.Error("display picture uploaded to storage but DB update failed — orphaned object",
					"worker", workerID,
					"trainer_id", job.TrainerID.String(),
					"object_key", job.ObjectKey,
					"err", dbErr,
				)
			case rows == 0:
				u.log.Error("display picture uploaded but no trainer row matched — orphaned object",
					"worker", workerID,
					"trainer_id", job.TrainerID.String(),
					"object_key", job.ObjectKey,
				)
			default:
				u.log.Info("display picture uploaded",
					"worker", workerID,
					"trainer_id", job.TrainerID.String(),
					"object_key", job.ObjectKey,
					"attempt", attempt,
				)
			}
			return
		}

		lastErr = err
		u.log.Warn("display picture upload attempt failed",
			"worker", workerID,
			"trainer_id", job.TrainerID.String(),
			"attempt", attempt,
			"err", err,
		)

		if attempt < trainerDisplayPictureMaxAttempts {
			backoff := time.Duration(1<<(attempt-1)) * time.Second // 1s, 2s
			select {
			case <-time.After(backoff):
			case <-u.stopCh:
				return
			}
		}
	}

	u.log.Error("display picture upload failed after all retries",
		"trainer_id", job.TrainerID.String(),
		"object_key", job.ObjectKey,
		"attempts", trainerDisplayPictureMaxAttempts,
		"err", lastErr,
	)
}

func (u *TrainerDisplayPictureUploader) updateDisplayPicture(job TrainerDisplayPictureJob) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return u.q.UpdateTrainerDisplayPicture(ctx, db.UpdateTrainerDisplayPictureParams{
		ID:             job.TrainerID,
		DisplayPicture: sql.NullString{String: job.PublicURL, Valid: true},
	})
}

// Touch errors import so this file stays standalone-buildable even if the
// import gets pruned by a tool. (ErrQueueFull / ErrUploaderClosed live in
// uploader.go in the same package.)
var _ = errors.New
