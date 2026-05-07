package utils

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// APIResponse is an optional envelope compatible with the existing "status/message" pattern.
type APIResponse struct {
	Status  string `json:"status,omitempty"`  // "success" or "error"
	Message string `json:"message,omitempty"` // human-readable message
	Data    any    `json:"data,omitempty"`    // response payload
	Error   string `json:"error,omitempty"`   // alternative error field used in some handlers
}

// JSON writes payload as-is (no enforced envelope).
func JSON(c *gin.Context, code int, payload any) {
	c.JSON(code, payload)
}

// OK uses the "status/message/data" envelope.
func OK(c *gin.Context, message string, data any) {
	c.JSON(http.StatusOK, APIResponse{Status: "success", Message: message, Data: data})
}

// Created uses the "status/message/data" envelope with 201.
func Created(c *gin.Context, message string, data any) {
	c.JSON(http.StatusCreated, APIResponse{Status: "success", Message: message, Data: data})
}

// Error uses the "status/message" envelope (matches your AuthMiddleware errors).
func Error(c *gin.Context, code int, message string) {
	c.AbortWithStatusJSON(code, APIResponse{Status: "error", Message: message})
}

// ErrorField uses {"error": "..."} (matches your Recover middleware style).
func ErrorField(c *gin.Context, code int, errMsg string) {
	c.AbortWithStatusJSON(code, APIResponse{Error: errMsg})
}

// ValidationError is a convenience helper for field validation problems.
func ValidationError(c *gin.Context, message string, errors any) {
	c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
		"status":  "error",
		"message": message,
		"errors":  errors,
	})
}