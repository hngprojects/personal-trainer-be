package activities

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

const (
	// defaultLimit balances "enough rows to fill a phone screen" with
	// "doesn't OOM the API on a chatty admin dashboard". Pick the value
	// from the same family as the other listing endpoints in this
	// codebase so client-side pagination feels uniform.
	defaultLimit = 20
	// maxLimit caps an over-eager client (and protects the UNION
	// query, whose plan is fine but never cheap, from someone passing
	// limit=10000).
	maxLimit = 100
)

// trainerLookup is the slice of *db.Queries we actually need; defined
// as an interface so handler tests can stub it without spinning a real
// Postgres.
type trainerLookup interface {
	GetUserRoleByID(ctx context.Context, id uuid.UUID) (string, error)
}

type Handler struct {
	repo  Repository
	users trainerLookup
	log   *slog.Logger
}

func NewHandler(repo Repository, q *db.Queries, log *slog.Logger) *Handler {
	return &Handler{repo: repo, users: q, log: log}
}

// Register hooks the two endpoints onto a gin router group.
//
// authMw enforces "must be logged in" for both. The admin endpoint
// additionally gets adminMw — typically middleware.SuperAdminOnly,
// which gates by role and short-circuits on its own. We attach it on
// the route itself rather than relying on the global oapi-codegen
// middleware chain because these routes are hand-wired and never
// reach that chain.
func (h *Handler) Register(group *gin.RouterGroup, authMw gin.HandlerFunc, adminMw gin.HandlerFunc) {
	group.GET("/trainers/me/activities", authMw, h.listForTrainer)
	group.GET("/admin/activities", authMw, adminMw, h.listAll)
}

func (h *Handler) listForTrainer(c *gin.Context) {
	userID, ok := common.UserIDFromContext(c.Request.Context())
	if !ok || userID == uuid.Nil {
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}

	// Defensive role check — the route is auth-gated but anyone with a
	// session token can hit it. Reject non-trainer callers with 403 so
	// a client account doesn't get an empty feed they'd interpret as
	// "no activity yet."
	role, err := h.users.GetUserRoleByID(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusUnauthorized, api.NewError("user not found", api.CodeUnauthorized))
			return
		}
		h.log.Error("activities: role lookup failed", "err", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, api.NewError("internal error", api.CodeServerError))
		return
	}
	if role != "trainer" {
		c.JSON(http.StatusForbidden, api.NewError("only trainers can view their own activity feed", api.CodeForbidden))
		return
	}

	cursor, limit, perr := parseListParams(c)
	if perr != nil {
		c.JSON(http.StatusBadRequest, api.NewError(perr.Error(), api.CodeBadRequest))
		return
	}

	// Over-fetch by 1 to detect "more pages exist" without a separate
	// COUNT(*) query.
	items, err := h.repo.ListForTrainer(c.Request.Context(), userID, cursor, limit+1)
	if err != nil {
		h.log.Error("activities: list for trainer failed", "err", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to load activities", api.CodeServerError))
		return
	}

	resp := paginate(items, limit)
	c.JSON(http.StatusOK, api.NewSuccess("ok", api.CodeOK, resp))
}

func (h *Handler) listAll(c *gin.Context) {
	cursor, limit, perr := parseListParams(c)
	if perr != nil {
		c.JSON(http.StatusBadRequest, api.NewError(perr.Error(), api.CodeBadRequest))
		return
	}
	items, err := h.repo.ListAll(c.Request.Context(), cursor, limit+1)
	if err != nil {
		h.log.Error("activities: list all failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to load activities", api.CodeServerError))
		return
	}
	resp := paginate(items, limit)
	c.JSON(http.StatusOK, api.NewSuccess("ok", api.CodeOK, resp))
}

// parseListParams extracts ?limit + ?cursor with sane defaults. Errors
// are user-facing strings — they go straight into a 400 body.
func parseListParams(c *gin.Context) (*Cursor, int, error) {
	limit := defaultLimit
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return nil, 0, errors.New("limit must be a positive integer")
		}
		if n > maxLimit {
			n = maxLimit
		}
		limit = n
	}
	var cursor *Cursor
	if raw := c.Query("cursor"); raw != "" {
		dec, err := DecodeCursor(raw)
		if err != nil {
			return nil, 0, errors.New("invalid cursor")
		}
		cursor = &dec
	}
	return cursor, limit, nil
}

// paginate trims the over-fetched extra row and computes next_cursor.
// Splitting this out makes the two handlers symmetric and the rule
// (over-fetch by 1) easy to verify in tests.
func paginate(items []Activity, limit int) ListResponse {
	if len(items) > limit {
		last := items[limit-1]
		return ListResponse{
			Items: items[:limit],
			NextCursor: Cursor{
				OccurredAt: last.OccurredAt,
				ActivityID: last.ID,
			}.Encode(),
		}
	}
	return ListResponse{Items: items}
}
