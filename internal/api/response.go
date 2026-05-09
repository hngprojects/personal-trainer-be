package api

import "fmt"

func NewSuccessResponse(message, code string, data interface{}, meta interface{}) SuccessResponse {
	resp := SuccessResponse{
		Code:    code,
		Message: message,
		Status:  "success",
	}

	if data != nil {
		// allow object OR array OR any JSON value
		resp.Data = &data
	}
	if meta != nil {
		resp.Meta = &meta
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
		Status:  "error",
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
