package routes_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// countTrainerAvailability returns how many trainer_availability rows
// exist for the trainer — used by tests that assert add/delete-one
// semantics without trusting only the response body.
func countTrainerAvailability(t *testing.T, db *sql.DB, trainerID string) int {
	t.Helper()
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM trainer_availability WHERE trainer_id = $1`, trainerID).Scan(&n)
	require.NoError(t, err)
	return n
}

// countBookingSlots is the discovery-side equivalent — no trainer filter
// because the bulk endpoint creates rows without a trainer association.
func countBookingSlots(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM booking_slots`).Scan(&n)
	require.NoError(t, err)
	return n
}

// TestTrainerAvailabilityAdditiveFlow pins the regression that motivated
// this PR: calling create-availability a second time used to wipe the
// first call's slots. POST is now additive; PUT remains the destructive
// "replace whole schedule" path. DELETE removes a single slot.
func TestTrainerAvailabilityAdditiveFlow(t *testing.T) {
	db, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)

	resetTables(t, db)

	trainerUserID := insertUser(t, db, "trainer@avail.test", "Trainer", "client")
	trainerID := insertTrainerWithSpecs(t, db, trainerUserID, "approved")
	trainerToken := tokenFor(t, trainerUserID)

	srv := newServer(t, db)
	t.Cleanup(srv.Close)
	httpClient := http.DefaultClient
	apiBase := srv.URL + "/api/v1"

	// 1: POST adds the first batch.
	{
		body := map[string]any{
			"availability": []map[string]any{
				{"day_of_week": 1, "start_time": "09:00", "end_time": "10:00", "timezone": "UTC"},
				{"day_of_week": 2, "start_time": "09:00", "end_time": "10:00", "timezone": "UTC"},
			},
		}
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, apiBase+"/trainers/me/availability", bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+trainerToken)
		req.Header.Set("Content-Type", "application/json")
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusCreated, res.StatusCode)
		require.Equal(t, 2, countTrainerAvailability(t, db, trainerID))
	}

	// 2: POST again — the original two must survive. This is the bug fix.
	{
		body := map[string]any{
			"availability": []map[string]any{
				{"day_of_week": 3, "start_time": "09:00", "end_time": "10:00", "timezone": "UTC"},
			},
		}
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, apiBase+"/trainers/me/availability", bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+trainerToken)
		req.Header.Set("Content-Type", "application/json")
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusCreated, res.StatusCode)
		require.Equal(t, 3, countTrainerAvailability(t, db, trainerID),
			"second POST must NOT wipe previous slots")
	}

	// 3: POST with a slot that overlaps an existing one → 400.
	{
		body := map[string]any{
			"availability": []map[string]any{
				{"day_of_week": 1, "start_time": "09:30", "end_time": "10:30", "timezone": "UTC"},
			},
		}
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, apiBase+"/trainers/me/availability", bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+trainerToken)
		req.Header.Set("Content-Type", "application/json")
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
		require.Equal(t, 3, countTrainerAvailability(t, db, trainerID))
	}

	// 4: POST exact duplicate of an existing tuple → 409. The handler
	// checks for exact (day, start, end) match BEFORE the general
	// overlap predicate, so duplicates get the precise status.
	{
		body := map[string]any{
			"availability": []map[string]any{
				{"day_of_week": 1, "start_time": "09:00", "end_time": "10:00", "timezone": "UTC"},
			},
		}
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, apiBase+"/trainers/me/availability", bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+trainerToken)
		req.Header.Set("Content-Type", "application/json")
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusConflict, res.StatusCode,
			"exact-duplicate of an existing slot must be 409, not the generic 400 from the overlap check")
		require.Equal(t, 3, countTrainerAvailability(t, db, trainerID))
	}

	// 4b: POST with two exact duplicates inside the same request body
	// → 409 (in-request duplicate detection mirrors the cross-existing
	// rule).
	{
		body := map[string]any{
			"availability": []map[string]any{
				{"day_of_week": 6, "start_time": "08:00", "end_time": "09:00", "timezone": "UTC"},
				{"day_of_week": 6, "start_time": "08:00", "end_time": "09:00", "timezone": "UTC"},
			},
		}
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, apiBase+"/trainers/me/availability", bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+trainerToken)
		req.Header.Set("Content-Type", "application/json")
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusConflict, res.StatusCode)
		require.Equal(t, 3, countTrainerAvailability(t, db, trainerID),
			"in-request duplicate must short-circuit before any insert")
	}

	// 5: DELETE one slot — others must remain. Pull a slot id off the
	// GET response so we don't have to query the DB.
	var slotID string
	{
		req, _ := http.NewRequest(http.MethodGet, apiBase+"/trainers/me/availability", nil)
		req.Header.Set("Authorization", "Bearer "+trainerToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)
		// GET returns AvailabilitySlot[] which doesn't include id today —
		// pull straight from the DB instead.
		err := db.QueryRow(`SELECT id::text FROM trainer_availability WHERE trainer_id = $1 ORDER BY day_of_week ASC LIMIT 1`, trainerID).Scan(&slotID)
		require.NoError(t, err)
	}
	{
		req, _ := http.NewRequest(http.MethodDelete, apiBase+"/trainers/me/availability/"+slotID, nil)
		req.Header.Set("Authorization", "Bearer "+trainerToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusNoContent, res.StatusCode)
		require.Equal(t, 2, countTrainerAvailability(t, db, trainerID),
			"DELETE one must leave the other slots alone")
	}

	// 6: DELETE again with the same id → 404 (already gone).
	{
		req, _ := http.NewRequest(http.MethodDelete, apiBase+"/trainers/me/availability/"+slotID, nil)
		req.Header.Set("Authorization", "Bearer "+trainerToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusNotFound, res.StatusCode)
	}

	// 7: DELETE someone else's slot → 404 (data-layer authz). Seed a
	// second trainer with a slot, then have trainer A try to delete it.
	{
		otherUserID := insertUser(t, db, "other@avail.test", "Other Trainer", "client")
		otherTrainerID := insertTrainerWithSpecs(t, db, otherUserID, "approved")
		var otherSlotID string
		err := db.QueryRow(`
INSERT INTO trainer_availability (trainer_id, day_of_week, start_time, end_time, timezone)
VALUES ($1, 4, '09:00', '10:00', 'UTC') RETURNING id::text`, otherTrainerID).Scan(&otherSlotID)
		require.NoError(t, err)

		req, _ := http.NewRequest(http.MethodDelete, apiBase+"/trainers/me/availability/"+otherSlotID, nil)
		req.Header.Set("Authorization", "Bearer "+trainerToken)
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusNotFound, res.StatusCode,
			"deleting another trainer's slot must 404, not 200/403 — data-layer authz check")
		require.Equal(t, 1, countTrainerAvailability(t, db, otherTrainerID),
			"the other trainer's slot must still be there")
	}

	// 8: PUT still replaces the whole schedule (regression guard for the
	// PUT path — the additive POST must NOT have changed PUT semantics).
	{
		body := map[string]any{
			"availability": []map[string]any{
				{"day_of_week": 5, "start_time": "12:00", "end_time": "13:00", "timezone": "UTC"},
			},
		}
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPut, apiBase+"/trainers/me/availability", bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+trainerToken)
		req.Header.Set("Content-Type", "application/json")
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusOK, res.StatusCode)
		require.Equal(t, 1, countTrainerAvailability(t, db, trainerID),
			"PUT must still replace — only the one slot from the body should remain")
	}
}

// TestDiscoverySlotsBulkCreate exercises the new POST /discovery-slots/bulk
// endpoint: happy path, per-slot validation rollback, and 400 on empty.
func TestDiscoverySlotsBulkCreate(t *testing.T) {
	db, cleanup := setupTestDB(t)
	t.Cleanup(cleanup)

	resetTables(t, db)
	mustExec(t, db, `TRUNCATE TABLE booking_slots RESTART IDENTITY CASCADE;`)

	superAdminID := insertSuperAdmin(t, db, "super@bulk.test", "Super")
	clientID := insertUser(t, db, "client@bulk.test", "Client", "client")
	superToken := tokenFor(t, superAdminID)
	clientToken := tokenFor(t, clientID)

	srv := newServer(t, db)
	t.Cleanup(srv.Close)
	httpClient := http.DefaultClient
	apiBase := srv.URL + "/api/v1"

	bulkURL := apiBase + "/discovery-slots/bulk"

	// 1: happy path — 3 distinct slots inserted.
	{
		body := map[string]any{
			"slots": []map[string]any{
				{"day_of_week": 1, "start_time": "09:00", "end_time": "10:00", "timezone": "Africa/Lagos"},
				{"day_of_week": 1, "start_time": "11:00", "end_time": "12:00", "timezone": "Africa/Lagos"},
				{"day_of_week": 2, "start_time": "14:00", "end_time": "15:00", "timezone": "Africa/Lagos"},
			},
		}
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, bulkURL, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+superToken)
		req.Header.Set("Content-Type", "application/json")
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusCreated, res.StatusCode)
		require.Equal(t, 3, countBookingSlots(t, db))
	}

	// 2: in-request overlap — whole batch rolled back. Pre-count must
	// match post-count.
	pre := countBookingSlots(t, db)
	{
		body := map[string]any{
			"slots": []map[string]any{
				{"day_of_week": 3, "start_time": "09:00", "end_time": "11:00", "timezone": "Africa/Lagos"},
				{"day_of_week": 3, "start_time": "10:00", "end_time": "12:00", "timezone": "Africa/Lagos"},
			},
		}
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, bulkURL, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+superToken)
		req.Header.Set("Content-Type", "application/json")
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
		require.Equal(t, pre, countBookingSlots(t, db),
			"in-request overlap must roll back the entire batch")
	}

	// 3: empty slots array → 400.
	{
		body := map[string]any{"slots": []any{}}
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, bulkURL, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+superToken)
		req.Header.Set("Content-Type", "application/json")
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
	}

	// 4: non-admin gets 403.
	{
		body := map[string]any{
			"slots": []map[string]any{
				{"day_of_week": 4, "start_time": "09:00", "end_time": "10:00", "timezone": "Africa/Lagos"},
			},
		}
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, bulkURL, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+clientToken)
		req.Header.Set("Content-Type", "application/json")
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusForbidden, res.StatusCode)
	}

	// 5: per-slot validation — bad start_time format. Pre/post DB count
	// must be equal (TX rollback).
	pre2 := countBookingSlots(t, db)
	{
		body := map[string]any{
			"slots": []map[string]any{
				{"day_of_week": 5, "start_time": "09:00", "end_time": "10:00", "timezone": "Africa/Lagos"},
				{"day_of_week": 5, "start_time": "not-a-time", "end_time": "11:00", "timezone": "Africa/Lagos"},
			},
		}
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, bulkURL, bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+superToken)
		req.Header.Set("Content-Type", "application/json")
		res := doReq(t, httpClient, req)
		defer func() { _ = res.Body.Close() }()
		require.Equal(t, http.StatusBadRequest, res.StatusCode)
		require.Equal(t, pre2, countBookingSlots(t, db),
			"per-slot validation failure must roll back the whole batch")
	}
}

// minute is a tiny sanity helper kept here so the test file compiles
// even if a future change drops its sole user — we silence the import.
var _ = fmt.Sprintf
var _ = time.Now
