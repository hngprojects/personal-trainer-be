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
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/storage"
)

// AvatarJob carries everything the worker needs to upload one avatar.
// Bytes are held in memory until the job is drained — the queue is bounded to
// keep total memory usage predictable (see Enqueue / ErrQueueFull below).
//
// ObjectKey and PublicURL are deliberately separate: ObjectKey is the
// bucket-relative path passed to PutObject (e.g. "avatars/{uid}/{uuid}.jpg"),
// while PublicURL is the fully-qualified URL the client will fetch and the
// value we write to users.avatar_url. Conflating the two led to a bug where
// the URL was used as the key, creating objects with paths like
// "https://cdn.example.com/avatars/.../foo.jpg" inside the bucket.
type AvatarJob struct {
	UserID      uuid.UUID
	ObjectKey   string // bucket-relative path, passed verbatim to Storage.PutObject
	PublicURL   string // what gets written to users.avatar_url on success
	ContentType string // MIME of the bytes
	Bytes       []byte // the actual file content
}

// ErrQueueFull is returned by Enqueue when the worker channel is at capacity.
// Handlers should map this to 503 with a retry hint — backpressure is what
// keeps MinIO and the DB from being overwhelmed by a burst.
var ErrQueueFull = errors.New("uploads: queue is full")

// ErrUploaderClosed is returned by Enqueue when the uploader has already been
// stopped (server shutdown in progress). Without this guard, a request that
// arrives during the gap between Stop() acquiring its lock and close(jobs)
// happening would panic with "send on closed channel".
var ErrUploaderClosed = errors.New("uploads: uploader is closed")

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

	// mu guards `closed`. Enqueue takes the read lock to check the flag
	// before sending; Stop takes the write lock to set the flag and close
	// the channel atomically. This prevents Enqueue from racing close(jobs)
	// and panicking with "send on closed channel".
	mu     sync.RWMutex
	closed bool
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
//
// Safe to call concurrently with Enqueue (sync.RWMutex serialises the close
// with any in-flight Enqueue) and safe to call multiple times (the closed
// flag prevents double-close).
//
// stopCh is closed BEFORE wg.Wait() so workers parked in retry-backoff
// (`time.After(backoff)` inside process()) can short-circuit via the
// `<-u.stopCh` select branch and exit immediately. If we closed stopCh after
// the wait, a worker mid-backoff would sleep the full 1s/2s/4s before
// noticing shutdown — extending termination by up to 7 seconds per worker.
func (u *AvatarUploader) Stop() {
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

// Enqueue submits a job non-blockingly. Returns:
//   - ErrUploaderClosed if Stop() has been called (server shutting down)
//   - ErrQueueFull if the buffer is saturated
//
// Callers should map both to 503 with a retry hint — backpressure protects
// MinIO/DB during bursts, and serving 503 during a shutdown window is correct.
func (u *AvatarUploader) Enqueue(job AvatarJob) error {
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
			// Storage write succeeded. Now update the DB. Three outcomes:
			//   1. rows == 1 — clean success.
			//   2. rows == 0 — user was deleted between the upload and the
			//      DB write. The object exists but no row points to it; record
			//      the failure so ops can clean up the orphan.
			//   3. err != nil — DB error. Object is uploaded but link failed;
			//      same treatment — record the failure.
			rows, dbErr := u.updateAvatar(job)
			switch {
			case dbErr != nil:
				u.log.Error("avatar uploaded to storage but DB update failed",
					"worker", workerID,
					"user_id", job.UserID.String(),
					"object_key", job.ObjectKey,
					"err", dbErr,
				)
				u.recordFailure(job, attempt, fmt.Errorf("storage ok; db update failed: %w", dbErr))
			case rows == 0:
				u.log.Error("avatar uploaded to storage but no user row matched — orphaned object",
					"worker", workerID,
					"user_id", job.UserID.String(),
					"object_key", job.ObjectKey,
				)
				u.recordFailure(job, attempt, errors.New("storage ok; user row not found (deleted between upload and db write?)"))
			default:
				u.log.Info("avatar uploaded",
					"worker", workerID,
					"user_id", job.UserID.String(),
					"object_key", job.ObjectKey,
					"attempt", attempt,
				)
			}
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
	u.recordFailure(job, maxAttempts, lastErr)
}

// updateAvatar writes the public URL into users.avatar_url. Returns the number
// of rows affected so the caller can distinguish "user gone" from "updated".
//
// NOTE: We use a fresh context (not derived from the HTTP request) because by
// the time the worker runs, the HTTP request that enqueued the job has long
// since returned a 202 to the client and its context has been canceled.
func (u *AvatarUploader) updateAvatar(job AvatarJob) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return u.q.UpdateUserAvatar(ctx, db.UpdateUserAvatarParams{
		ID:        job.UserID,
		AvatarUrl: sql.NullString{String: job.PublicURL, Valid: true},
	})
}

// recordFailure persists the terminal-failure marker. attempts is the number
// of upload tries actually performed (1..maxAttempts) — passed in so DB-link
// failures after a successful storage upload record their real attempt count
// (often 1) rather than always maxAttempts.
//
// If the DB write itself fails, log it loudly — there's nothing else we can
// do, but silently dropping would defeat the "don't drop" guarantee.
func (u *AvatarUploader) recordFailure(job AvatarJob, attempts int, lastErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := u.q.RecordFailedAvatarUpload(ctx, db.RecordFailedAvatarUploadParams{
		UserID:    job.UserID,
		ObjectKey: job.ObjectKey,
		Attempts:  int32(attempts),
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
	u.log.Error("avatar upload failed — recorded for ops",
		"user_id", job.UserID.String(),
		"object_key", job.ObjectKey,
		"attempts", attempts,
		"err", lastErr,
	)
}
