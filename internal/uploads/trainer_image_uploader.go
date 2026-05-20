package uploads

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/storage"
)

// TrainerImageJob carries one gallery-image upload. Same in-memory shape as
// AvatarJob — images are small enough (max 5 MiB enforced at handler) that
// bytes-in-channel is fine, and we don't need the disk-backed dance of the
// video pipeline.
type TrainerImageJob struct {
	TrainerID   uuid.UUID
	ObjectKey   string // bucket-relative path
	PublicURL   string // what gets INSERTed into trainer_images.image_url
	ContentType string
	Bytes       []byte
}

// TrainerImageUploader runs the gallery-image background pipeline: upload
// to MinIO → INSERT row in trainer_images. Mirrors AvatarUploader almost
// 1:1; the only meaningful difference is the DB call (INSERT instead of
// UPDATE).
type TrainerImageUploader struct {
	store storage.Storage
	q     *db.Queries
	log   *slog.Logger
	jobs  chan TrainerImageJob
	wg    sync.WaitGroup

	mu     sync.RWMutex
	closed bool
	stopCh chan struct{}
}

func NewTrainerImageUploader(store storage.Storage, q *db.Queries, log *slog.Logger, bufferSize int) *TrainerImageUploader {
	return &TrainerImageUploader{
		store:  store,
		q:      q,
		log:    log,
		jobs:   make(chan TrainerImageJob, bufferSize),
		stopCh: make(chan struct{}),
	}
}

func (u *TrainerImageUploader) Start(workers int) {
	for i := 0; i < workers; i++ {
		u.wg.Add(1)
		go u.run(i)
	}
}

// Stop matches AvatarUploader.Stop: close stopCh BEFORE wg.Wait so any
// worker parked in backoff can short-circuit immediately.
func (u *TrainerImageUploader) Stop() {
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

func (u *TrainerImageUploader) Enqueue(job TrainerImageJob) error {
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

// EnqueueBatch is the atomic all-or-nothing variant. Either every job in
// the slice is accepted, or none — never a partial accept. Used by the
// trainer-images batch upload handler so a mid-batch ErrQueueFull doesn't
// leave the request in a state where the client thinks the upload failed
// but workers are already processing the first half.
//
// Takes the write lock so the capacity check and the sends happen
// atomically with respect to other Enqueue/EnqueueBatch callers (workers
// drain concurrently — that's OK, it just frees up space, never adds).
// Since the lock is held only for the few microseconds of the sends, this
// doesn't meaningfully serialise the producer path.
func (u *TrainerImageUploader) EnqueueBatch(jobs []TrainerImageJob) error {
	if len(jobs) == 0 {
		return nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.closed {
		return ErrUploaderClosed
	}
	// Workers can only DRAIN the channel concurrently, never add — so if
	// there's room now, all len(jobs) sends will succeed without blocking.
	if cap(u.jobs)-len(u.jobs) < len(jobs) {
		return ErrQueueFull
	}
	for _, j := range jobs {
		u.jobs <- j
	}
	return nil
}

func (u *TrainerImageUploader) run(workerID int) {
	defer u.wg.Done()
	for job := range u.jobs {
		u.process(workerID, job)
	}
}

const (
	trainerImageMaxAttempts    = 3
	trainerImageAttemptTimeout = 30 * time.Second
)

func (u *TrainerImageUploader) process(workerID int, job TrainerImageJob) {
	var lastErr error
	for attempt := 1; attempt <= trainerImageMaxAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), trainerImageAttemptTimeout)
		err := u.store.PutObject(ctx, job.ObjectKey, bytes.NewReader(job.Bytes), int64(len(job.Bytes)), job.ContentType)
		cancel()

		if err == nil {
			// Storage succeeded — INSERT the trainer_images row. The
			// insertImage call has its own bounded retry loop for
			// transient DB errors (connection blips, serialisation
			// failures); only FK violations (trainer deleted) and the
			// 5-cap trigger violation are treated as terminal because
			// those will never resolve via retry.
			if dbErr := u.insertImageWithRetry(workerID, job); dbErr != nil {
				u.log.Error("trainer image uploaded to storage but DB insert failed — orphaned object",
					"worker", workerID,
					"trainer_id", job.TrainerID.String(),
					"object_key", job.ObjectKey,
					"err", dbErr,
				)
				// No failed_trainer_image_uploads table — the orphan is
				// logged loudly. Adding a tracking table is a follow-up if
				// ops asks for it.
				return
			}
			u.log.Info("trainer image uploaded",
				"worker", workerID,
				"trainer_id", job.TrainerID.String(),
				"object_key", job.ObjectKey,
				"attempt", attempt,
			)
			return
		}

		lastErr = err
		u.log.Warn("trainer image upload attempt failed",
			"worker", workerID,
			"trainer_id", job.TrainerID.String(),
			"attempt", attempt,
			"err", err,
		)

		if attempt < trainerImageMaxAttempts {
			backoff := time.Duration(1<<(attempt-1)) * time.Second // 1s, 2s
			select {
			case <-time.After(backoff):
			case <-u.stopCh:
				return
			}
		}
	}

	u.log.Error("trainer image upload failed after all retries",
		"trainer_id", job.TrainerID.String(),
		"object_key", job.ObjectKey,
		"attempts", trainerImageMaxAttempts,
		"err", lastErr,
	)
}

// insertImageWithRetry runs the AddTrainerImage INSERT, distinguishing
// transient errors (network blip, lock conflict) — which we retry with
// short backoff — from terminal ones (FK violation = trainer deleted,
// check_violation = 5-image cap hit). Without this split, a momentary DB
// hiccup after a successful storage upload would silently orphan the
// object in MinIO and lose the gallery entry.
func (u *TrainerImageUploader) insertImageWithRetry(workerID int, job TrainerImageJob) error {
	const dbMaxAttempts = 3
	var lastErr error
	for attempt := 1; attempt <= dbMaxAttempts; attempt++ {
		err := u.insertImage(job)
		if err == nil {
			return nil
		}
		lastErr = err

		// Terminal? Stop immediately — retrying won't change the answer.
		if isTerminalDBError(err) {
			return err
		}

		u.log.Warn("trainer image DB insert attempt failed (will retry)",
			"worker", workerID,
			"trainer_id", job.TrainerID.String(),
			"attempt", attempt,
			"err", err,
		)
		if attempt < dbMaxAttempts {
			backoff := time.Duration(1<<(attempt-1)) * 200 * time.Millisecond // 200ms, 400ms
			select {
			case <-time.After(backoff):
			case <-u.stopCh:
				return lastErr
			}
		}
	}
	return lastErr
}

func (u *TrainerImageUploader) insertImage(job TrainerImageJob) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := u.q.AddTrainerImage(ctx, db.AddTrainerImageParams{
		TrainerID: job.TrainerID,
		ImageUrl:  job.PublicURL,
	})
	if err != nil {
		return fmt.Errorf("insert trainer_images row: %w", err)
	}
	return nil
}

// isTerminalDBError returns true for PostgreSQL error codes that will never
// resolve on retry, so the worker should give up immediately and surface
// the failure. Everything else is treated as transient (network/lock blip,
// retry-with-backoff worth attempting).
//
// Terminal codes for the trainer_images insert path:
//   - 23503 foreign_key_violation: trainer row gone (deleted between upload
//     start and DB write)
//   - 23514 check_violation: the 5-cap trigger fired (which uses
//     ERRCODE='check_violation' explicitly)
//   - 23505 unique_violation: would only fire if the per-trainer advisory
//     lock failed somehow; retrying won't help — same position next time
func isTerminalDBError(err error) bool {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		return false
	}
	switch pqErr.Code {
	case "23503", "23514", "23505":
		return true
	}
	return false
}

// Silence unused-import linter when this file builds standalone — errors is
// referenced via the var declarations in uploader.go (ErrUploaderClosed,
// ErrQueueFull) in the same package, so keep this import-aware no-op.
var _ = errors.New
