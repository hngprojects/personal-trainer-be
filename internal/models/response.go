package models

type SuccessResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Data    any    `json:"data"`
	Code    string `json:"code"`
	Meta    any    `json:"meta"`
}

type ErrorResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Code    string `json:"code"`
	Errors  []any  `json:"errors"`
}
