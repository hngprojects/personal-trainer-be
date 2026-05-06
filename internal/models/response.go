package models

type SuccessResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    any    `json:"data"`
	Meta    any    `json:"meta"`
}

type ErrorResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Errors  []any  `json:"errors"`
}
