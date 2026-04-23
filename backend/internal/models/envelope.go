package models

// Envelope is the standard API response wrapper.
type Envelope struct {
	Data     interface{}        `json:"data"`
	Error    *string            `json:"error"`
	Meta     interface{}        `json:"meta"`
	Warnings []EnvelopeWarning  `json:"warnings,omitempty"`
}

// EnvelopeWarning is a non-fatal advisory the server emits alongside a 2xx
// response. Used by Path B S2 to surface stale-CLI-health and connector-
// outdated signals at run-creation time without failing the request
// (design §6.2). Other handlers may reuse the shape for similar soft
// signals as long as the `code` value is documented in api-surface.md.
type EnvelopeWarning struct {
	Code    string                 `json:"code"`
	Message string                 `json:"message,omitempty"`
	Details map[string]interface{} `json:"details,omitempty"`
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

// SuccessResponseWithWarnings wraps a 2xx payload with optional non-fatal
// warnings. When `warnings` is empty the JSON output is byte-identical to
// SuccessResponse (the field is `omitempty`), so existing clients are
// unaffected.
func SuccessResponseWithWarnings(data interface{}, meta interface{}, warnings []EnvelopeWarning) Envelope {
	return Envelope{Data: data, Error: nil, Meta: meta, Warnings: warnings}
}

func ErrorResponse(message string) Envelope {
	return Envelope{Data: nil, Error: &message, Meta: nil}
}
