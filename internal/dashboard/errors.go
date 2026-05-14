package dashboard

import (
	"encoding/json"
	"net/http"

	"tunneledge/pkg/errs"

	"github.com/rs/zerolog/log"
)

type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, APIError{Code: status, Message: message})
}

// writeServiceError maps domain errs.Code to HTTP status and logs accordingly:
//   - 5xx (unexpected internal errors) → Error
//   - 401/403 (auth denials)           → Warn  (useful for security auditing)
//   - 404                              → Debug (normal in API flows)
//   - 400/409 (client mistakes)        → nothing (already returned to caller)
func writeServiceError(r *http.Request, w http.ResponseWriter, err error) {
	requestID := RequestIDFromContext(r.Context())
	code := errs.GetCode(err)

	switch code {
	case errs.CodeInvalidArg:
		writeError(w, http.StatusBadRequest, err.Error())

	case errs.CodeUnauthorized:
		log.Warn().
			Str("request_id", requestID).
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Str("remote_addr", r.RemoteAddr).
			Err(err).
			Msg("unauthorized")
		writeError(w, http.StatusUnauthorized, err.Error())

	case errs.CodeForbidden:
		log.Warn().
			Str("request_id", requestID).
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Str("remote_addr", r.RemoteAddr).
			Err(err).
			Msg("forbidden")
		writeError(w, http.StatusForbidden, err.Error())

	case errs.CodeNotFound:
		log.Debug().
			Str("request_id", requestID).
			Str("path", r.URL.Path).
			Err(err).
			Msg("not found")
		writeError(w, http.StatusNotFound, err.Error())

	case errs.CodeAlreadyExists:
		writeError(w, http.StatusConflict, err.Error())

	default:
		// Unexpected internal error — always log at Error so it surfaces in prod.
		log.Error().
			Str("request_id", requestID).
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Str("remote_addr", r.RemoteAddr).
			Err(err).
			Msg("internal service error")
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}
