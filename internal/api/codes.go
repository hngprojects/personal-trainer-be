package api

const (
	// Success
	CodeOK       = "OK"
	CodeCreated  = "CREATED"
	CodeAccepted = "ACCEPTED" // long-running work was queued; check back later

	// Client Errors
	CodeBadRequest      = "BAD_REQUEST"
	CodeUnauthorized    = "UNAUTHORIZED"
	CodeForbidden       = "FORBIDDEN"
	CodeNotFound        = "NOT_FOUND"
	CodeConflict        = "CONFLICT"
	CodeTooManyRequests = "TOO_MANY_REQUESTS"
	CodeInvalidInput    = "INVALID_INPUT"
	CodePaymentFailed   = "PAYMENT_FAILED"
	CodeRateLimited     = "RATE_LIMITED"

	// Server Error
	CodeServerError    = "SERVER_ERROR"
	CodeInternalError  = "INTERNAL_ERROR"
	CodeNotImplemented = "NOT_IMPLEMENTED"
)
