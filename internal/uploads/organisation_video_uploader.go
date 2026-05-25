package uploads

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"

	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/storage"
	"github.com/hngprojects/personal-trainer-be/pkg/video"
)

// OrganisationVideoJob carries one organisation_media video upload.
// Like VideoJob (trainer intro flow), this holds a disk path rather
// than bytes — videos are too large to fan out across the queue depth
// in RAM. The DB row is already INSERTed (status='processing') by the
// handler; this worker transcodes + uploads + flips status.
type OrganisationVideoJob struct {
	MediaID       uuid.UUID
	ObjectKey     string
	TempInputPath string
	ContentType   string // always "video/mp4" after transcode
}

const (
	orgVideoMaxAttempts     = 3
	orgVideoAttemptTimeout  = 5 * time.Minute
	orgVideoUploadTimeout   = 60 * time.Second
)

// OrganisationVideoUploader mirrors VideoUploader. Different fields
// it touches (organisation_media instead of trainers.intro_video_url),
// but same retry / temp-file / transcode-then-upload shape.
type OrganisationVideoUploader struct {
	store      storage.Storage
	transcoder video.Transcoder
	q          *db.Queries
	log        *slog.Logger
	jobs       chan OrganisationVideoJob
	wg         sync.WaitGroup
	stopCh     chan struct{}

	mu     sync.RWMutex
	closed bool
}

func NewOrganisationVideoUploader(store storage.Storage, transcoder video.Transcoder, q *db.Queries, log *slog.Logger, bufferSize int) *OrganisationVideoUploader {
	return &OrganisationVideoUploader{
		store:      store,
		transcoder: transcoder,
		q:          q,
		log:        log,
		jobs:       make(chan OrganisationVideoJob, bufferSize),
		stopCh:     make(chan struct{}),
	}
}

func (u *OrganisationVideoUploader) Start(workers int) {
	for i := 0; i < workers; i++ {
		u.wg.Add(1)
		go u.run(i)
	}
}

func (u *OrganisationVideoUploader) Stop() {
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

func (u *OrganisationVideoUploader) Enqueue(job OrganisationVideoJob) error {
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

func (u *OrganisationVideoUploader) run(workerID int) {
	defer u.wg.Done()
	for job := range u.jobs {
		u.process(workerID, job)
	}
}

func (u *OrganisationVideoUploader) process(workerID int, job OrganisationVideoJob) {
	// Original input cleanup is unconditional. The transcoded output is
	// cleaned per-attempt inside attempt().
	defer func() {
		if err := os.Remove(job.TempInputPath); err != nil && !os.IsNotExist(err) {
			u.log.Warn("organisation video: failed to delete temp input", "path", job.TempInputPath, "err", err)
		}
	}()

	var lastErr error
	for attempt := 1; attempt <= orgVideoMaxAttempts; attempt++ {
		err := u.attempt(workerID, job, attempt)
		if err == nil {
			return // success path logs inside attempt()
		}
		lastErr = err
		if attempt < orgVideoMaxAttempts {
			backoff := time.Duration(1<<(attempt-1)) * time.Second // 1s, 2s
			select {
			case <-time.After(backoff):
			case <-u.stopCh:
				return
			}
		}
	}

	u.markStatus(workerID, job.MediaID, "failed")
	u.log.Error("organisation video upload failed after all retries",
		"media_id", job.MediaID.String(),
		"object_key", job.ObjectKey,
		"attempts", orgVideoMaxAttempts,
		"err", lastErr,
	)
}

func (u *OrganisationVideoUploader) attempt(workerID int, job OrganisationVideoJob, attempt int) error {
	dstPath := job.TempInputPath + ".transcoded." + uuid.NewString() + ".mp4"
	defer func() {
		if err := os.Remove(dstPath); err != nil && !os.IsNotExist(err) {
			u.log.Warn("organisation video: failed to delete transcoded output", "path", dstPath, "err", err)
		}
	}()

	transcodeCtx, cancelTranscode := context.WithTimeout(context.Background(), orgVideoAttemptTimeout)
	tErr := u.transcoder.Transcode(transcodeCtx, job.TempInputPath, dstPath)
	cancelTranscode()
	if tErr != nil {
		u.log.Warn("organisation video: transcode attempt failed",
			"worker", workerID, "media_id", job.MediaID.String(),
			"attempt", attempt, "err", tErr)
		return fmt.Errorf("transcode: %w", tErr)
	}

	f, err := os.Open(dstPath)
	if err != nil {
		return fmt.Errorf("open transcoded file: %w", err)
	}
	defer func() { _ = f.Close() }()

	stat, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat transcoded file: %w", err)
	}

	uploadCtx, cancelUpload := context.WithTimeout(context.Background(), orgVideoUploadTimeout)
	uErr := u.store.PutObject(uploadCtx, job.ObjectKey, f, stat.Size(), job.ContentType)
	cancelUpload()
	if uErr != nil {
		u.log.Warn("organisation video: minio upload attempt failed",
			"worker", workerID, "media_id", job.MediaID.String(),
			"attempt", attempt, "err", uErr)
		return fmt.Errorf("storage upload: %w", uErr)
	}

	u.markStatus(workerID, job.MediaID, "ready")
	u.log.Info("organisation video: transcoded and uploaded",
		"worker", workerID, "media_id", job.MediaID.String(),
		"object_key", job.ObjectKey, "size_bytes", stat.Size(), "attempt", attempt)
	return nil
}

func (u *OrganisationVideoUploader) markStatus(workerID int, mediaID uuid.UUID, status string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := u.q.UpdateOrganisationMediaStatus(ctx, db.UpdateOrganisationMediaStatusParams{
		ID:     mediaID,
		Status: status,
	})
	if err != nil {
		u.log.Error("organisation video: status update failed",
			"worker", workerID, "media_id", mediaID.String(), "status", status, "err", err)
		return
	}
	if rows == 0 {
		// Row was deleted while we were processing — not an error.
		u.log.Info("organisation video: status update skipped (row deleted)",
			"worker", workerID, "media_id", mediaID.String())
	}
}

// Silence unused-import linter — errors referenced via package vars.
var _ = errors.New
