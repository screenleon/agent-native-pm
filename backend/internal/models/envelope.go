package models

// Envelope is the standard API response wrapper.
type Envelope struct {
	Data  interface{} `json:"data"`
	Error *string     `json:"error"`
	Meta  interface{} `json:"meta"`
}

// PaginationMeta holds pagination information.
type PaginationMeta struct {
	Page    int `json:"page"`
	PerPage int `json:"per_page"`
	Total   int `json:"total"`
}

func SuccessResponse(data interface{}, meta interface{}) Envelope {
	return Envelope{Data: data, Error: nil, Meta: meta}
}

func ErrorResponse(message string) Envelope {
	return Envelope{Data: nil, Error: &message, Meta: nil}
}
