package api

const (
	// Success
	CodeOK      = "OK"
	CodeCreated = "CREATED"

	// Client Errors
	CodeBadRequest      = "BAD_REQUEST"
	CodeUnauthorized    = "UNAUTHORIZED"
	CodeForbidden       = "FORBIDDEN"
	CodeNotFound        = "NOT_FOUND"
	CodeConflict        = "CONFLICT"
	CodeTooManyRequests = "TOO_MANY_REQUESTS"

	// Server Error
	CodeServerError    = "SERVER_ERROR"
	CodeNotImplemented = "NOT_IMPLEMENTED"
)
