package uploads

import (
	"context"
	"database/sql"
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

// VideoJob carries everything the worker needs to transcode + upload one
// trainer-intro video. Unlike AvatarJob this holds disk paths, not bytes —
// videos are too large to hold in memory across the queue depth (a 100MB
// video × 20-deep buffer × 2 workers would be ~4GB resident).
//
// ObjectKey is the bucket-relative path passed to Storage.PutObject.
// PublicURL is what gets written to trainers.intro_video_url on success.
// TempInputPath is the path to the original upload on local disk; the
// worker reads it, transcodes to a temp output, then deletes both.
type VideoJob struct {
	TrainerID     uuid.UUID
	ObjectKey     string
	PublicURL     string
	TempInputPath string
	ContentType   string // always "video/mp4" after transcode; kept here in case the type evolves
}

const (
	// Same retry budget as the avatar pipeline. Videos take longer per
	// attempt so the absolute wait between attempts matters less.
	videoMaxAttempts = 3

	// 5 minutes per attempt. Transcoding a 10-minute 4K input on 2 CPUs
	// can take 3-4 minutes; this gives headroom without leaving a hung
	// ffmpeg to pin the worker forever.
	videoAttemptTimeout = 5 * time.Minute

	// MinIO upload + DB write are fast — separate, shorter budget.
	videoUploadTimeout = 60 * time.Second
)

// VideoUploader runs the trainer-intro-video pipeline: transcode with
// ffmpeg → upload to MinIO → update DB. Worker count is intentionally low
// (default 2) because ffmpeg is CPU-bound — more workers just thrash.
type VideoUploader struct {
	store      storage.Storage
	transcoder video.Transcoder
	q          *db.Queries
	log        *slog.Logger
	jobs       chan VideoJob
	wg         sync.WaitGroup
	stopCh     chan struct{}

	mu     sync.RWMutex
	closed bool
}

func NewVideoUploader(store storage.Storage, transcoder video.Transcoder, q *db.Queries, log *slog.Logger, bufferSize int) *VideoUploader {
	return &VideoUploader{
		store:      store,
		transcoder: transcoder,
		q:          q,
		log:        log,
		jobs:       make(chan VideoJob, bufferSize),
		stopCh:     make(chan struct{}),
	}
}

// Start spawns `workers` goroutines that drain the job channel. Call once.
// Use a small number (2 is the default we wire) — ffmpeg saturates a CPU
// core per process and more workers just contend for it.
func (u *VideoUploader) Start(workers int) {
	for i := 0; i < workers; i++ {
		u.wg.Add(1)
		go u.run(i)
	}
}

// Stop is identical in shape to AvatarUploader.Stop — race-safe close,
// stopCh closed before wg.Wait so backoff sleeps cancel immediately.
func (u *VideoUploader) Stop() {
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

// Enqueue submits a job. Returns ErrUploaderClosed during shutdown or
// ErrQueueFull when the buffer is saturated — handler maps both to 503.
//
// On a full queue OR closed uploader, the caller is responsible for
// deleting job.TempInputPath. The handler returns 503 so the user knows
// to retry, but the temp file we wrote in the handler still needs to be
// cleaned up. See trainer_video.go for the cleanup wiring.
func (u *VideoUploader) Enqueue(job VideoJob) error {
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

func (u *VideoUploader) run(workerID int) {
	defer u.wg.Done()
	for job := range u.jobs {
		u.process(workerID, job)
	}
}

// process handles a single video job:
//   1. Transcode to a temp output path
//   2. Upload the transcoded file to MinIO
//   3. UPDATE trainers.intro_video_url
//   4. Delete BOTH temp files (regardless of success/failure)
//
// Retries are budgeted per "transcode + upload" attempt, not per substep —
// if MinIO is flaky we re-transcode each retry, which is wasteful but
// keeps the retry semantics simple and matches the avatar pipeline.
func (u *VideoUploader) process(workerID int, job VideoJob) {
	// Always clean up the original input — it's served its purpose
	// regardless of outcome. The transcoded output is cleaned per-attempt
	// inside the loop.
	defer func() {
		if err := os.Remove(job.TempInputPath); err != nil && !os.IsNotExist(err) {
			u.log.Warn("video: failed to delete temp input", "path", job.TempInputPath, "err", err)
		}
	}()

	var lastErr error
	for attempt := 1; attempt <= videoMaxAttempts; attempt++ {
		err := u.attempt(workerID, job, attempt)
		if err == nil {
			return // success path logged inside attempt()
		}
		lastErr = err

		// Exponential backoff before retrying. Skip the sleep on the final
		// attempt — there's no next retry.
		if attempt < videoMaxAttempts {
			backoff := time.Duration(1<<(attempt-1)) * time.Second // 1s, 2s
			select {
			case <-time.After(backoff):
			case <-u.stopCh:
				return // server shutting down — abandon
			}
		}
	}
	u.recordFailure(job, videoMaxAttempts, lastErr)
}

// attempt runs one transcode + upload + DB-write cycle. Returns nil on
// success. The transcoded output file is always deleted before this
// function returns (success OR failure).
func (u *VideoUploader) attempt(workerID int, job VideoJob, attempt int) error {
	// Distinct temp filename per attempt so a partial output from the
	// previous try can't confuse this one (ffmpeg's -y would overwrite
	// but we'd rather not rely on that).
	dstPath := job.TempInputPath + ".transcoded." + uuid.NewString() + ".mp4"
	defer func() {
		if err := os.Remove(dstPath); err != nil && !os.IsNotExist(err) {
			u.log.Warn("video: failed to delete transcoded output", "path", dstPath, "err", err)
		}
	}()

	transcodeCtx, cancelTranscode := context.WithTimeout(context.Background(), videoAttemptTimeout)
	tErr := u.transcoder.Transcode(transcodeCtx, job.TempInputPath, dstPath)
	cancelTranscode()
	if tErr != nil {
		u.log.Warn("video: transcode attempt failed",
			"worker", workerID, "trainer_id", job.TrainerID.String(),
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

	uploadCtx, cancelUpload := context.WithTimeout(context.Background(), videoUploadTimeout)
	uErr := u.store.PutObject(uploadCtx, job.ObjectKey, f, stat.Size(), job.ContentType)
	cancelUpload()
	if uErr != nil {
		u.log.Warn("video: minio upload attempt failed",
			"worker", workerID, "trainer_id", job.TrainerID.String(),
			"attempt", attempt, "err", uErr)
		return fmt.Errorf("storage upload: %w", uErr)
	}

	// Storage succeeded — same rows/error tri-state as AvatarUploader so
	// "trainer deleted between upload and write" is recorded loudly, not
	// silently orphaned.
	rows, dbErr := u.updateTrainerVideo(job)
	switch {
	case dbErr != nil:
		u.log.Error("video: uploaded to storage but DB update failed",
			"worker", workerID, "trainer_id", job.TrainerID.String(),
			"object_key", job.ObjectKey, "err", dbErr)
		u.recordFailure(job, attempt, fmt.Errorf("storage ok; db update failed: %w", dbErr))
		return nil // don't retry; storage already has it, retry just orphans more
	case rows == 0:
		u.log.Error("video: uploaded to storage but no trainer row matched — orphaned object",
			"worker", workerID, "trainer_id", job.TrainerID.String(),
			"object_key", job.ObjectKey)
		u.recordFailure(job, attempt, errors.New("storage ok; trainer row not found (deleted between upload and db write?)"))
		return nil
	default:
		u.log.Info("video: transcoded and uploaded",
			"worker", workerID, "trainer_id", job.TrainerID.String(),
			"object_key", job.ObjectKey, "size_bytes", stat.Size(), "attempt", attempt)
		return nil
	}
}

func (u *VideoUploader) updateTrainerVideo(job VideoJob) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return u.q.UpdateTrainerIntroVideo(ctx, db.UpdateTrainerIntroVideoParams{
		ID:            job.TrainerID,
		IntroVideoUrl: sql.NullString{String: job.PublicURL, Valid: true},
	})
}

func (u *VideoUploader) recordFailure(job VideoJob, attempts int, lastErr error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := u.q.RecordFailedVideoUpload(ctx, db.RecordFailedVideoUploadParams{
		TrainerID: job.TrainerID,
		ObjectKey: job.ObjectKey,
		Attempts:  int32(attempts),
		LastError: lastErr.Error(),
	}); err != nil {
		u.log.Error("FAILED to record failed video upload — dropping silently",
			"trainer_id", job.TrainerID.String(),
			"object_key", job.ObjectKey,
			"upload_err", lastErr,
			"db_err", err,
		)
		return
	}
	u.log.Error("video upload failed — recorded for ops",
		"trainer_id", job.TrainerID.String(),
		"object_key", job.ObjectKey,
		"attempts", attempts,
		"err", lastErr,
	)
}
