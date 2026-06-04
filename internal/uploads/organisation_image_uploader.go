package uploads

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/storage"
)

// OrganisationImageJob carries one organisation_media image upload. The
// DB row is already INSERTed (status='processing') by the handler — the
// worker only needs to push bytes to MinIO and flip the status. Bytes
// live in memory because the per-file cap (5 MiB, enforced at handler)
// keeps total queue residency bounded.
type OrganisationImageJob struct {
	MediaID     uuid.UUID
	ObjectKey   string
	ContentType string
	Bytes       []byte
}

// OrganisationImageUploader mirrors TrainerImageUploader but writes to
// organisation_media.status instead of inserting a trainer_images row.
// The handler does the INSERT up front so the public_url returned in
// the 202 is reachable as soon as the worker uploads; this worker just
// flips status: 'processing' -> 'ready' on success, 'failed' after
// retries exhaust.
type OrganisationImageUploader struct {
	store storage.Storage
	q     *db.Queries
	log   *slog.Logger
	jobs  chan OrganisationImageJob
	wg    sync.WaitGroup

	mu     sync.RWMutex
	closed bool
	stopCh chan struct{}
}

func NewOrganisationImageUploader(store storage.Storage, q *db.Queries, log *slog.Logger, bufferSize int) *OrganisationImageUploader {
	return &OrganisationImageUploader{
		store:  store,
		q:      q,
		log:    log,
		jobs:   make(chan OrganisationImageJob, bufferSize),
		stopCh: make(chan struct{}),
	}
}

func (u *OrganisationImageUploader) Start(workers int) {
	for i := 0; i < workers; i++ {
		u.wg.Add(1)
		go u.run(i)
	}
}

// Stop closes the job channel and waits for in-flight uploads to
// finish. stopCh closes BEFORE wg.Wait so workers parked in retry
// backoff exit immediately.
func (u *OrganisationImageUploader) Stop() {
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

func (u *OrganisationImageUploader) Enqueue(job OrganisationImageJob) error {
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

func (u *OrganisationImageUploader) run(workerID int) {
	defer u.wg.Done()
	for job := range u.jobs {
		u.process(workerID, job)
	}
}

const (
	orgImageMaxAttempts    = 3
	orgImageAttemptTimeout = 30 * time.Second
)

func (u *OrganisationImageUploader) process(workerID int, job OrganisationImageJob) {
	var lastErr error
	for attempt := 1; attempt <= orgImageMaxAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), orgImageAttemptTimeout)
		err := u.store.PutObject(ctx, job.ObjectKey, bytes.NewReader(job.Bytes), int64(len(job.Bytes)), job.ContentType)
		cancel()

		if err == nil {
			u.markStatus(workerID, job.MediaID, "ready")
			u.log.Info("organisation image uploaded",
				"worker", workerID,
				"media_id", job.MediaID.String(),
				"object_key", job.ObjectKey,
				"attempt", attempt,
			)
			return
		}

		lastErr = err
		u.log.Warn("organisation image upload attempt failed",
			"worker", workerID,
			"media_id", job.MediaID.String(),
			"attempt", attempt,
			"err", err,
		)

		if attempt < orgImageMaxAttempts {
			backoff := time.Duration(1<<(attempt-1)) * time.Second // 1s, 2s
			select {
			case <-time.After(backoff):
			case <-u.stopCh:
				return
			}
		}
	}

	// Exhausted retries — flip status to 'failed' so the admin sees it
	// in the list with status filter and can clean up.
	u.markStatus(workerID, job.MediaID, "failed")
	u.log.Error("organisation image upload failed after all retries",
		"media_id", job.MediaID.String(),
		"object_key", job.ObjectKey,
		"attempts", orgImageMaxAttempts,
		"err", lastErr,
	)
}

// markStatus updates the row's status. Failure to update is logged but
// doesn't surface anywhere else — the row would just be left in
// 'processing' forever, which is visible in the list endpoint and the
// admin can sweep it via DELETE. Better to log than to retry-storm the
// DB on top of an already-failed upload.
func (u *OrganisationImageUploader) markStatus(workerID int, mediaID uuid.UUID, status string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := u.q.UpdateOrganisationMediaStatus(ctx, db.UpdateOrganisationMediaStatusParams{
		ID:     mediaID,
		Status: status,
	})
	if err != nil {
		u.log.Error("organisation image: status update failed",
			"worker", workerID, "media_id", mediaID.String(), "status", status, "err", err)
		return
	}
	if rows == 0 {
		// Row was deleted by an admin while we were processing. Not an
		// error — just note we skipped the status update.
		u.log.Info("organisation image: status update skipped (row deleted)",
			"worker", workerID, "media_id", mediaID.String())
	}
}

// Silence unused-import linter — errors is referenced via the package
// vars (ErrQueueFull, ErrUploaderClosed) declared in uploader.go.
var _ = errors.New
