package routes_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// multipartBody builds a minimal POST /trainers form body. Callers
// override only the fields they're testing; the helper supplies the
// required ones (email/name/specializations) and lets the caller pass
// optional ones (gender, phone_number) via the extra map.
func multipartCreateTrainerBody(t *testing.T, email, name string, extra map[string]string) (body *bytes.Buffer, contentType string) {
	t.Helper()
	b := &bytes.Buffer{}
	w := multipart.NewWriter(b)
	must := func(err error) {
		t.Helper()
		require.NoError(t, err)
	}
	must(w.WriteField("email", email))
	must(w.WriteField("name", name))
	must(w.WriteField("specializations", "strength"))
	for k, v := range extra {
		must(w.WriteField(k, v))
	}
	require.NoError(t, w.Close())
	return b, w.FormDataContentType()
}

// TestCreateTrainer_GenderPhone exercises the new gender + phone_number
// inputs on POST /trainers — the round-trip into the response payload
// and follow-up GET, plus the two validation paths (invalid enum,
// invalid E.164 format).
//
// Skipped unless RUN_INTEGRATION_TESTS=1 + DATABASE_URL is set — same
// gating as the other integration suites in this package.
func TestCreateTrainer_GenderPhone(t *testing.T) {
	db, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)

	resetTables(t, db)

	superAdminID := insertSuperAdmin(t, db, "super@gp.test", "Super Admin")
	superToken := tokenFor(t, superAdminID)

	srv := newServer(t, db)
	t.Cleanup(srv.Close)
	httpClient := http.DefaultClient
	apiBase := srv.URL + "/api/v1"

	var newTrainerID string
	t.Run("create: gender + phone echoed in body, also persists for GET /trainers/{id}", func(t *testing.T) {
		body, ct := multipartCreateTrainerBody(t, "trip@gp.test", "Trip Trainer", map[string]string{
			"gender":       "male",
			"phone_number": "+2348087654321",
		})
		req, _ := http.NewRequest(http.MethodPost, apiBase+"/trainers", body)
		req.Header.Set("Authorization", "Bearer "+superToken)
		req.Header.Set("Content-Type", ct)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusCreated, res.StatusCode)

		var resp struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, json.NewDecoder(res.Body).Decode(&resp))
		require.Equal(t, "male", resp.Data["gender"], "gender should round-trip on create response")
		require.Equal(t, "+2348087654321", resp.Data["phone_number"])

		idStr, ok := resp.Data["id"].(string)
		require.True(t, ok, "data.id missing or not a string")
		newTrainerID = idStr

		// Follow-up GET /trainers/{id} must surface the same values.
		req2, _ := http.NewRequest(http.MethodGet, apiBase+"/trainers/"+newTrainerID, nil)
		req2.Header.Set("Authorization", "Bearer "+superToken)
		res2 := doReq(t, httpClient, req2)
		defer func() { _ = res2.Body.Close() }()
		require.Equal(t, http.StatusOK, res2.StatusCode)

		var getResp struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, json.NewDecoder(res2.Body).Decode(&getResp))
		require.Equal(t, "male", getResp.Data["gender"], "GET /trainers/{id} must include gender")
		require.Equal(t, "+2348087654321", getResp.Data["phone_number"])
	})

	t.Run("create: omitting gender + phone leaves them null on response", func(t *testing.T) {
		body, ct := multipartCreateTrainerBody(t, "nullable@gp.test", "Nullable Trainer", nil)
		req, _ := http.NewRequest(http.MethodPost, apiBase+"/trainers", body)
		req.Header.Set("Authorization", "Bearer "+superToken)
		req.Header.Set("Content-Type", ct)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusCreated, res.StatusCode)

		var resp struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, json.NewDecoder(res.Body).Decode(&resp))
		// Both fields are emitted with JSON null when missing, mirroring
		// how bio/years_of_experience are returned. FE should treat
		// these as "not set yet."
		require.Contains(t, resp.Data, "gender")
		require.Nil(t, resp.Data["gender"])
		require.Contains(t, resp.Data, "phone_number")
		require.Nil(t, resp.Data["phone_number"])
	})

	t.Run("create: invalid gender enum returns 400", func(t *testing.T) {
		body, ct := multipartCreateTrainerBody(t, "badg@gp.test", "Bad Gender", map[string]string{
			"gender": "robot",
		})
		req, _ := http.NewRequest(http.MethodPost, apiBase+"/trainers", body)
		req.Header.Set("Authorization", "Bearer "+superToken)
		req.Header.Set("Content-Type", ct)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("create: malformed phone (not E.164) returns 400", func(t *testing.T) {
		body, ct := multipartCreateTrainerBody(t, "badp@gp.test", "Bad Phone", map[string]string{
			"phone_number": "0801-234-5678", // local format, no leading +CC
		})
		req, _ := http.NewRequest(http.MethodPost, apiBase+"/trainers", body)
		req.Header.Set("Authorization", "Bearer "+superToken)
		req.Header.Set("Content-Type", ct)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	// PATCH /trainers/{id} must surface gender + phone_number too —
	// before this fix the handler returned trainerToMap(updated) which
	// only sees the trainers row, so users-side fields drifted off the
	// PATCH response even though GET /trainers/{id} included them.
	t.Run("update: PATCH response keeps gender + phone from prior create", func(t *testing.T) {
		require.NotEmpty(t, newTrainerID, "depends on earlier create subtest")
		patchBody, _ := json.Marshal(map[string]any{
			"bio": "updated bio",
		})
		req, _ := http.NewRequest(http.MethodPatch, apiBase+"/trainers/"+newTrainerID, bytes.NewReader(patchBody))
		req.Header.Set("Authorization", "Bearer "+superToken)
		req.Header.Set("Content-Type", "application/json")
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)

		var resp struct {
			Data map[string]any `json:"data"`
		}
		require.NoError(t, json.NewDecoder(res.Body).Decode(&resp))
		require.Equal(t, "updated bio", resp.Data["bio"])
		require.Equal(t, "male", resp.Data["gender"],
			"PATCH response must include gender (seeded by the earlier create); the regression check for the shared payload builder")
		require.Equal(t, "+2348087654321", resp.Data["phone_number"])
	})
}

