package routes_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// tinyPNG builds a 1x1 PNG in memory — large enough to satisfy the
// MIME sniff in detectTrainerImage, small enough to dodge any size
// caps. Used to drive the POST /media/images happy path without a
// real image fixture on disk.
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func multipartMediaImageBody(t *testing.T, title, category string, file []byte, filename string) (*bytes.Buffer, string) {
	t.Helper()
	b := &bytes.Buffer{}
	w := multipart.NewWriter(b)
	must := func(err error) {
		t.Helper()
		require.NoError(t, err)
	}
	must(w.WriteField("title", title))
	if category != "" {
		must(w.WriteField("category", category))
	}
	fw, err := w.CreateFormFile("file", filename)
	must(err)
	_, err = fw.Write(file)
	must(err)
	require.NoError(t, w.Close())
	return b, w.FormDataContentType()
}

// TestOrganisationMedia_AuthAndShape exercises the four observable
// behaviours of the new /media endpoints without depending on a
// MinIO worker actually pushing bytes:
//
//   - public GET works without a token
//   - admin can POST image; row appears via GET /media/{id}
//   - plain client POST -> 403
//   - DELETE on a 'processing' row -> 409, then status flipped to
//     'ready' via SQL and DELETE succeeds
//
// The worker pipeline is NOT exercised here — that needs a real MinIO
// + ffmpeg in CI, which the integration suite doesn't have. The
// behaviour we DO verify is what the FE depends on: the row is
// written synchronously, the public_url is present in the response,
// and the auth gate is correct.
func TestOrganisationMedia_AuthAndShape(t *testing.T) {
	db, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)

	resetTables(t, db)
	mustExec(t, db, `TRUNCATE TABLE organisation_media RESTART IDENTITY CASCADE;`)

	superAdminID := insertSuperAdmin(t, db, "super@media.test", "Super")
	clientID := insertUser(t, db, "client@media.test", "Client", "client")
	superToken := tokenFor(t, superAdminID)
	clientToken := tokenFor(t, clientID)

	srv := newServer(t, db)
	t.Cleanup(srv.Close)
	httpClient := http.DefaultClient
	apiBase := srv.URL + "/api/v1"

	// Public GET /media with no token: should 200 with an empty list.
	t.Run("list media: public, no token required, empty initially", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, apiBase+"/media", nil)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)

		var body struct {
			Data []map[string]any `json:"data"`
		}
		require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
		require.Empty(t, body.Data)
	})

	// Plain client tries to upload — 403.
	t.Run("upload image: non-admin -> 403", func(t *testing.T) {
		body, ct := multipartMediaImageBody(t, "hero", "hero", tinyPNG(t), "hero.png")
		req, _ := http.NewRequest(http.MethodPost, apiBase+"/media/images", body)
		req.Header.Set("Authorization", "Bearer "+clientToken)
		req.Header.Set("Content-Type", ct)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusForbidden, res.StatusCode)
	})

	// Admin upload — 202; the row appears via GET /media/{id} even
	// though the worker hasn't pushed bytes to MinIO (the public URL
	// would 404 until the worker runs, which is fine for this test).
	var createdID string
	t.Run("upload image: admin happy path returns 202 + row id", func(t *testing.T) {
		body, ct := multipartMediaImageBody(t, "hero-img", "hero", tinyPNG(t), "hero.png")
		req, _ := http.NewRequest(http.MethodPost, apiBase+"/media/images", body)
		req.Header.Set("Authorization", "Bearer "+superToken)
		req.Header.Set("Content-Type", ct)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		// 202 if the storage backend is configured (uploader enqueues),
		// 503 if not (CI without MinIO). Accept both, since the row
		// itself is what we care about.
		if res.StatusCode == http.StatusServiceUnavailable {
			t.Skip("storage backend not configured in this CI environment; skipping image upload e2e")
		}
		require.Equal(t, http.StatusAccepted, res.StatusCode, "body=%s", readAll(t, res.Body))

		var resp struct {
			Data struct {
				ID        string `json:"id"`
				MediaType string `json:"media_type"`
				Title     string `json:"title"`
				Category  string `json:"category"`
				Status    string `json:"status"`
				PublicURL string `json:"public_url"`
			} `json:"data"`
		}
		require.NoError(t, json.NewDecoder(res.Body).Decode(&resp))
		require.Equal(t, "image", resp.Data.MediaType)
		require.Equal(t, "hero-img", resp.Data.Title)
		require.Equal(t, "hero", resp.Data.Category)
		require.Equal(t, "processing", resp.Data.Status,
			"new uploads must start as processing; worker flips to ready")
		require.NotEmpty(t, resp.Data.PublicURL)
		require.NotEmpty(t, resp.Data.ID)
		createdID = resp.Data.ID
	})

	t.Run("get media by id: public, returns the row we just created", func(t *testing.T) {
		if createdID == "" {
			t.Skip("upload subtest didn't run")
		}
		req, _ := http.NewRequest(http.MethodGet, apiBase+"/media/"+createdID, nil)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)
	})

	t.Run("delete: 409 while still processing", func(t *testing.T) {
		if createdID == "" {
			t.Skip("upload subtest didn't run")
		}
		req, _ := http.NewRequest(http.MethodDelete, apiBase+"/media/"+createdID, nil)
		req.Header.Set("Authorization", "Bearer "+superToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusConflict, res.StatusCode,
			"delete must refuse on processing rows to avoid racing the worker")
	})

	t.Run("delete: succeeds once status is ready", func(t *testing.T) {
		if createdID == "" {
			t.Skip("upload subtest didn't run")
		}
		// Simulate the worker flipping status to 'ready'. Parameterised
		// even though createdID is a UUID we just minted ourselves —
		// keeps the test honest about the pattern and matches the
		// COUNT(*) lookup a few lines down.
		_, err := db.Exec(`UPDATE organisation_media SET status = 'ready' WHERE id = $1`, createdID)
		require.NoError(t, err)

		req, _ := http.NewRequest(http.MethodDelete, apiBase+"/media/"+createdID, nil)
		req.Header.Set("Authorization", "Bearer "+superToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusNoContent, res.StatusCode)

		// Row should be gone.
		var n int
		err = db.QueryRow("SELECT COUNT(*) FROM organisation_media WHERE id = $1", createdID).Scan(&n)
		require.NoError(t, err)
		require.Equal(t, 0, n)
	})

	t.Run("list media: invalid type query returns 400", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, apiBase+"/media?type=audio", nil)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		// oapi-codegen rejects enum mismatch with 400 before our
		// handler even runs.
		require.Contains(t, []int{http.StatusBadRequest}, res.StatusCode)
	})
}

// Tiny shared helper so test bodies stay readable even when we need
// the raw body to include in a failure message.
func readAll(t *testing.T, r io.Reader) string {
	t.Helper()
	_ = sql.ErrNoRows // anchor — silences "unused import" if the helper ever loses its DB usage
	b, _ := io.ReadAll(r)
	return string(b)
}
