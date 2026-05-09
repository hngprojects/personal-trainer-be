package routes

import (
    "errors"
    "net/http"

    "github.com/gin-gonic/gin"
    "github.com/google/uuid"
    "github.com/hngprojects/personal-trainer-be/internal/api"
    errs "github.com/hngprojects/personal-trainer-be/pkg/errors"
)

func (s *routerImpl) CreateAdminInvite(c *gin.Context) {
    if !s.requireSuperAdmin(c) {
        return
    }

    var req struct {
        Email string `json:"email" binding:"required,email"`
        Name  string `json:"name"  binding:"required"`
    }
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, api.NewError("invalid input", api.CodeBadRequest))
        return
    }

    invitedBy := c.MustGet("user_id").(uuid.UUID)

    err := s.invites.Create(c.Request.Context(), invitedBy, req.Email, req.Name)
    switch {
    case errors.Is(err, errs.ErrConflict):
        c.JSON(http.StatusConflict, api.NewError("email already exists or has a pending invite", api.CodeConflict))
    case err != nil:
        s.log.Error("create invite failed", "err", err, "email", req.Email)
        c.JSON(http.StatusInternalServerError, api.NewError("server error", api.CodeServerError))
    default:
        c.JSON(http.StatusCreated, api.NewSuccessResponse("invite sent", api.CodeCreated, nil, nil))
    }
}

func (s *routerImpl) GetAdminInvite(c *gin.Context, token string) {
    if s.invites == nil {
        c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
        return
    }
    res, err := s.invites.Validate(c.Request.Context(), token)
    if err != nil {
        c.JSON(http.StatusNotFound, api.NewError("invite invalid or expired", api.CodeNotFound))
        return
    }
    c.JSON(http.StatusOK, api.NewSuccessResponse("invite valid", api.CodeOK, map[string]interface{}{
        "email": res.Email, "name": res.Name,
    }, nil))
}

func (s *routerImpl) AcceptAdminInvite(c *gin.Context, token string) {
    if s.invites == nil {
        c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
        return
    }
    res, err := s.invites.Accept(c.Request.Context(), token)
    if err != nil {
        // Always 404 for any failure mode — enumeration resistance.
        c.JSON(http.StatusNotFound, api.NewError("invite invalid or expired", api.CodeNotFound))
        return
    }
    c.JSON(http.StatusOK, api.NewSuccessResponse("admin account created", api.CodeOK, map[string]interface{}{
        "user": map[string]interface{}{
            "id": res.UserID, "email": res.Email, "name": res.Name,
            "user_type": "admin", "profile_complete": false,
        },
        "password":      res.GeneratedPassword,
        "access_token":  res.AccessToken,
        "refresh_token": res.RefreshToken,
    }, nil))
}
