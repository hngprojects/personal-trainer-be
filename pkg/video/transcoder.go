// Package video wraps the ffmpeg / ffprobe CLI binaries for transcoding and
// metadata extraction. We shell out rather than use a Go binding because the
// only credible video encoders are C libraries (libx264, libavcodec) — there's
// no pure-Go option, and CGO bindings add deploy complexity without value.
//
// The deploy host must have `ffmpeg` and `ffprobe` on PATH. If they're
// missing, callers should detect that at startup and wire NoopTranscoder so
// the upload endpoint returns 503 instead of crashing per-request.
package video

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
)

// ErrNotConfigured is returned by NoopTranscoder. Callers should detect this
// at the handler boundary and map to 503.
var ErrNotConfigured = errors.New("video: ffmpeg is not available on this host")

// Metadata is the subset of ffprobe output we actually use for validation
// (duration check, codec sanity). We deliberately don't parse the whole
// ffprobe blob — schema drift between ffmpeg versions has bitten projects
// that did.
type Metadata struct {
	DurationSeconds float64
	HasVideoStream  bool
}

// Transcoder is the consumer-side interface. Keep minimal — handler only
// needs Probe (pre-flight) and the uploader only needs Transcode.
type Transcoder interface {
	// Probe reads metadata from the source file without decoding the full
	// stream. Used by the HTTP handler to reject too-long or non-video
	// uploads before queueing.
	Probe(ctx context.Context, srcPath string) (Metadata, error)

	// Transcode re-encodes srcPath to dstPath as H.264/AAC MP4 with the
	// settings configured at construction time. Blocks until complete or
	// ctx is cancelled.
	Transcode(ctx context.Context, srcPath, dstPath string) error
}

// FFmpegTranscoder shells out to ffmpeg / ffprobe. Construct via
// NewFFmpegTranscoder; it probes for the binaries at startup so a missing
// install surfaces loudly instead of failing on the first upload.
type FFmpegTranscoder struct {
	ffmpegPath  string
	ffprobePath string

	// height of the output (e.g. 720). Width is computed from aspect ratio.
	// 720 covers cellular streaming without burning bandwidth on detail an
	// intro video doesn't need.
	targetHeight int

	// CRF is ffmpeg's quality knob for libx264. 23 is "visually lossless",
	// 28 is "good for web", 30+ shows compression artifacts. 28 is the
	// industry default for transcoded web video.
	crf int
}

// NewFFmpegTranscoder verifies that ffmpeg and ffprobe are on PATH. If either
// is missing, returns an error — caller should fall back to NoopTranscoder.
func NewFFmpegTranscoder() (*FFmpegTranscoder, error) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("video: ffmpeg not found on PATH: %w", err)
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil, fmt.Errorf("video: ffprobe not found on PATH: %w", err)
	}
	return &FFmpegTranscoder{
		ffmpegPath:   ffmpeg,
		ffprobePath:  ffprobe,
		targetHeight: 720,
		crf:          28,
	}, nil
}

// Probe runs `ffprobe -show_format -show_streams -of json` and pulls out
// the bits we use. Returns an error if the input is not parseable as media.
func (f *FFmpegTranscoder) Probe(ctx context.Context, srcPath string) (Metadata, error) {
	cmd := exec.CommandContext(ctx, f.ffprobePath,
		"-v", "error",
		"-show_format",
		"-show_streams",
		"-of", "json",
		srcPath,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Metadata{}, fmt.Errorf("video: ffprobe failed: %w (%s)", err, stderr.String())
	}

	var raw struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		return Metadata{}, fmt.Errorf("video: parse ffprobe output: %w", err)
	}

	meta := Metadata{}
	// Duration MUST parse. Silently defaulting to 0 lets oversized uploads
	// pass the handler's `duration > max` check because 0 < anything.
	// ffprobe returns an empty string here for non-media files; both empty
	// and malformed must fail the probe loudly.
	if raw.Format.Duration == "" {
		return Metadata{}, errors.New("video: ffprobe returned no duration (input is likely not a valid media file)")
	}
	d, err := strconv.ParseFloat(raw.Format.Duration, 64)
	if err != nil {
		return Metadata{}, fmt.Errorf("video: parse duration %q: %w", raw.Format.Duration, err)
	}
	meta.DurationSeconds = d

	for _, s := range raw.Streams {
		if s.CodecType == "video" {
			meta.HasVideoStream = true
			break
		}
	}
	return meta, nil
}

// Transcode re-encodes srcPath → dstPath as H.264/AAC MP4 at the configured
// height and CRF. Audio is re-encoded too (AAC 128k) for codec-uniformity.
// `-movflags +faststart` puts the moov atom at the start of the file so the
// browser/mobile player can begin playback before the full download.
func (f *FFmpegTranscoder) Transcode(ctx context.Context, srcPath, dstPath string) error {
	scale := fmt.Sprintf("scale=-2:%d", f.targetHeight) // -2 = preserve aspect, force even width
	cmd := exec.CommandContext(ctx, f.ffmpegPath,
		"-y",         // overwrite dst if it exists (worker controls the path so this is safe)
		"-i", srcPath,
		"-vf", scale,
		"-c:v", "libx264",
		"-preset", "medium",                // balance of speed vs compression
		"-crf", strconv.Itoa(f.crf),
		"-c:a", "aac",
		"-b:a", "128k",
		"-movflags", "+faststart",          // enables progressive playback
		"-loglevel", "error",
		dstPath,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("video: ffmpeg transcode failed: %w (%s)", err, stderr.String())
	}
	return nil
}

// NoopTranscoder is wired when ffmpeg is missing at startup so the rest of
// the app builds and starts. Every call returns ErrNotConfigured.
type NoopTranscoder struct{}

func (NoopTranscoder) Probe(_ context.Context, _ string) (Metadata, error) {
	return Metadata{}, ErrNotConfigured
}

func (NoopTranscoder) Transcode(_ context.Context, _, _ string) error {
	return ErrNotConfigured
}
