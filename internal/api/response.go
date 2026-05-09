package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

func NewSuccessResponse(message, code string, data interface{}, meta interface{}) SuccessResponse {
	resp := SuccessResponse{
		Code:    code,
		Message: message,
		Status:  SuccessResponseStatusSuccess,
	}
	if data != nil {
		var d map[string]interface{}
		switch v := data.(type) {
		case map[string]interface{}:
			d = v
		default:
			b, err := json.Marshal(v)
			if err != nil {
				slog.Error("failed to marshal response data", "err", err)
			} else if err := json.Unmarshal(b, &d); err != nil {
				slog.Error("failed to unmarshal response data", "err", err)
			}
		}
		if d != nil {
			resp.Data = &d
		}
	}
	if meta != nil {
		var m map[string]interface{}
		switch v := meta.(type) {
		case map[string]interface{}:
			m = v
		default:
			b, err := json.Marshal(v)
			if err != nil {
				slog.Error("failed to marshal response meta", "err", err)
			} else if err := json.Unmarshal(b, &m); err != nil {
				slog.Error("failed to unmarshal response meta", "err", err)
			}
		}
		if m != nil {
			resp.Meta = &m
		}
	}
	return resp
}

func NewErrorResponse(message, code string, errors []FieldError) ErrorResponse {
	var errs *[]FieldError
	if len(errors) > 0 {
		errs = &errors
	}
	return ErrorResponse{
		Code:    code,
		Message: message,
		Status:  ErrorResponseStatusError,
		Errors:  errs,
	}
}

func NewSuccess(message string, code string, data any) SuccessResponse {
	return NewSuccessResponse(message, code, data, nil)
}

func NewSuccessWithMeta(message string, code string, data interface{}, meta interface{}) SuccessResponse {
	return NewSuccessResponse(message, code, data, meta)
}

func NewError(message string, code string) ErrorResponse {
	return NewErrorResponse(message, code, nil)
}

func NewValidationError(errors []FieldError) ErrorResponse {
	return NewErrorResponse("Validation error", CodeBadRequest, errors)
}

func NewNotFoundError(resource string) ErrorResponse {
	return NewErrorResponse(resource+" not found", CodeNotFound, nil)
}

type PaginationMeta struct {
	Page       int    `json:"page"`
	PerPage    int    `json:"per_page"`
	TotalPages int    `json:"total_pages"`
	TotalCount int    `json:"total_count"`
	Next       string `json:"next,omitempty"`
	Prev       string `json:"prev,omitempty"`
}

func NewPaginationMeta(page, perPage, totalCount int) PaginationMeta {
	totalPages := 0
	if perPage > 0 {
		totalPages = (totalCount + perPage - 1) / perPage
	}
	meta := PaginationMeta{
		Page:       page,
		PerPage:    perPage,
		TotalPages: totalPages,
		TotalCount: totalCount,
	}
	if page < totalPages {
		meta.Next = fmt.Sprintf("?page=%d", page+1)
	}
	if page > 1 {
		meta.Prev = fmt.Sprintf("?page=%d", page-1)
	}
	return meta
}
