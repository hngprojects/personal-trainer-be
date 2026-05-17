// Package uploads runs the background avatar-upload pipeline. Handlers enqueue
// a job containing the raw bytes; a fixed worker pool drains the queue,
// uploads to object storage, and updates the user's avatar_url in the DB.
// Terminal failures are persisted to failed_avatar_uploads so they're visible
// to operators (bytes themselves are not retained — the user re-uploads).
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

// AvatarJob carries everything the worker needs to upload one avatar.
// Bytes are held in memory until the job is drained — the queue is bounded to
// keep total memory usage predictable (see Enqueue / QueueFull below).
type AvatarJob struct {
	UserID      uuid.UUID
	ObjectKey   string // e.g. "avatars/{uid}/{uuid}.jpg" — the final path inside the bucket
	ContentType string // MIME of the bytes
	Bytes       []byte // the actual file content
}

// QueueFull is returned by Enqueue when the worker channel is at capacity.
// Handlers should map this to 503 with a retry hint — backpressure is what
// keeps MinIO and the DB from being overwhelmed by a burst.
var QueueFull = errors.New("uploads: queue is full")

const (
	// retry budget for a single job. Total worst-case time per job before
	// terminal failure: 1s + 2s + 4s = 7 seconds of backoff plus three upload
	// attempts.
	maxAttempts = 3

	// per-attempt timeout — protects against a hung MinIO connection holding
	// a worker forever.
	attemptTimeout = 30 * time.Second
)

// AvatarUploader is the producer/consumer pipeline. Start once at app init,
// Stop once at shutdown for graceful drain.
type AvatarUploader struct {
	store  storage.Storage
	q      *db.Queries
	log    *slog.Logger
	jobs   chan AvatarJob
	wg     sync.WaitGroup
	stopCh chan struct{}
}

// NewAvatarUploader returns a configured uploader. bufferSize bounds in-flight
// memory (4 workers × 100-deep buffer × 5MB max ≈ 2GB worst case, in practice
// much lower because most uploads are <1MB).
func NewAvatarUploader(store storage.Storage, q *db.Queries, log *slog.Logger, bufferSize int) *AvatarUploader {
	return &AvatarUploader{
		store:  store,
		q:      q,
		log:    log,
		jobs:   make(chan AvatarJob, bufferSize),
		stopCh: make(chan struct{}),
	}
}

// Start spawns `workers` goroutines that drain the job channel. Call once.
func (u *AvatarUploader) Start(workers int) {
	for i := 0; i < workers; i++ {
		u.wg.Add(1)
		go u.run(i)
	}
}

// Stop closes the channel, waits for in-flight jobs to finish, and returns.
// Wired into Router.Close() for graceful shutdown — pending uploads at the
// moment of SIGTERM still get a chance to land.
func (u *AvatarUploader) Stop() {
	close(u.jobs)
	u.wg.Wait()
	close(u.stopCh)
}

// Enqueue submits a job non-blockingly. Returns QueueFull if the buffer is
// saturated, in which case the caller should reject the request (503) rather
// than block the HTTP handler.
func (u *AvatarUploader) Enqueue(job AvatarJob) error {
	select {
	case u.jobs <- job:
		return nil
	default:
		return QueueFull
	}
}

// run is the per-worker loop. Each worker pulls jobs until the channel is
// closed by Stop().
func (u *AvatarUploader) run(workerID int) {
	defer u.wg.Done()
	for job := range u.jobs {
		u.process(workerID, job)
	}
}

// process handles a single job: bounded retries with exponential backoff, then
// terminal-failure persistence. Each attempt has its own short context so a
// stuck connection doesn't pin the worker.
func (u *AvatarUploader) process(workerID int, job AvatarJob) {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), attemptTimeout)
		err := u.store.PutObject(ctx, job.ObjectKey, bytes.NewReader(job.Bytes), int64(len(job.Bytes)), job.ContentType)
		cancel()

		if err == nil {
			if err := u.updateAvatar(job); err != nil {
				// Storage succeeded but DB failed. The object IS uploaded —
				// log loudly so ops can either retry the DB write or
				// reconcile, but DON'T mark as a terminal failure (the
				// object is in the bucket, just orphaned from any user row).
				u.log.Error("avatar uploaded to storage but DB update failed",
					"worker", workerID,
					"user_id", job.UserID.String(),
					"object_key", job.ObjectKey,
					"err", err,
				)
				return
			}
			u.log.Info("avatar uploaded",
				"worker", workerID,
				"user_id", job.UserID.String(),
				"object_key", job.ObjectKey,
				"attempt", attempt,
			)
			return
		}

		lastErr = err
		u.log.Warn("avatar upload attempt failed",
			"worker", workerID,
			"user_id", job.UserID.String(),
			"attempt", attempt,
			"err", err,
		)

		// Exponential backoff before retrying. Skip the sleep on the final
		// attempt — there's no next retry.
		if attempt < maxAttempts {
			backoff := time.Duration(1<<(attempt-1)) * time.Second // 1s, 2s
			select {
			case <-time.After(backoff):
			case <-u.stopCh:
				return // server shutting down — abandon
			}
		}
	}

	// All retries exhausted. Persist the failure so it's visible.
	u.recordFailure(job, lastErr)
}

// updateAvatar writes the public URL into users.avatar_url. The URL is built
// by the caller (handler) and stored verbatim in ObjectKey here — but since
// the DB stores the public URL, the handler embeds it via a path scheme we
// reconstruct: caller passes ObjectKey, we expect the public-URL piece to be
// already present in the job. Simpler: store ObjectKey as-is; handler chose
// the URL it wants the client to see.
//
// NOTE: We use a fresh context (not derived from the HTTP request) because by
// the time the worker runs, the HTTP request that enqueued the job has long
// since returned a 202 to the client and its context has been canceled.
func (u *AvatarUploader) updateAvatar(job AvatarJob) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return u.q.UpdateUserAvatar(ctx, db.UpdateUserAvatarParams{
		ID:        job.UserID,
		AvatarUrl: sql.NullString{String: job.ObjectKey, Valid: true},
	})
}

// recordFailure persists the terminal-failure marker. If the DB write itself
// fails, log it — there's nothing else we can do, and silently dropping
// would defeat the "don't drop" guarantee.
func (u *AvatarUploader) recordFailure(job AvatarJob, lastErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := u.q.RecordFailedAvatarUpload(ctx, db.RecordFailedAvatarUploadParams{
		UserID:    job.UserID,
		ObjectKey: job.ObjectKey,
		Attempts:  int32(maxAttempts),
		LastError: lastErr.Error(),
	}); err != nil {
		u.log.Error("FAILED to record failed avatar upload — dropping silently",
			"user_id", job.UserID.String(),
			"object_key", job.ObjectKey,
			"upload_err", lastErr,
			"db_err", err,
		)
		return
	}
	u.log.Error("avatar upload failed after all retries — recorded for ops",
		"user_id", job.UserID.String(),
		"object_key", job.ObjectKey,
		"attempts", maxAttempts,
		"err", lastErr,
	)
}
