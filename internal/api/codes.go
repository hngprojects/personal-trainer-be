// api/codes.go 
package api

const (
	// Success
	CodeOK      = "OK"
	CodeCreated = "CREATED"

	// Client Errors
	CodeBadRequest   = "BAD_REQUEST"
	CodeUnauthorized = "UNAUTHORIZED"
	CodeForbidden    = "FORBIDDEN"
	CodeNotFound     = "NOT_FOUND"

	// Server Error
	CodeServerError    = "SERVER_ERROR"
	CodeNotImplemented = "NOT_IMPLEMENTED"
)
