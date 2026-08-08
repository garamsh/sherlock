package tool

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// Wire error kinds carried in wireError.Kind. The client reconstructs the
// matching typed error from the kind, so error identity survives the wire.
const (
	wireValidation = "validation"
	wireExecution  = "execution"
	wireNotFound   = "not_found"
	wireBadRequest = "bad_request"
)

// wireError is the single error envelope of the wire protocol: every
// non-success response body has this shape.
type wireError struct {
	Kind        string `json:"kind"`
	Tool        string `json:"tool,omitempty"`
	Description string `json:"description,omitempty"`
	Message     string `json:"message"`
}

// NewHandler returns an http.Handler serving reg over the tool wire
// protocol: GET /tools lists the registry's descriptors, and
// POST /tools/{name}/call invokes a tool with the request body as its JSON
// input, returning the tool's JSON output as the response body. Failures
// return a wireError envelope with a non-200 status.
func NewHandler(reg *Registry) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /tools", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, reg.List())
	})
	mux.HandleFunc("POST /tools/{name}/call", func(w http.ResponseWriter, r *http.Request) {
		serveCall(w, r, reg)
	})
	return mux
}

func serveCall(w http.ResponseWriter, r *http.Request, reg *Registry) {
	name := r.PathValue("name")
	var input json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, wireError{
			Kind:    wireBadRequest,
			Tool:    name,
			Message: fmt.Sprintf("malformed JSON body: %v", err),
		})
		return
	}
	t, err := reg.Lookup(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, wireError{
			Kind:    wireNotFound,
			Tool:    name,
			Message: err.Error(),
		})
		return
	}
	out, err := Invoke(r.Context(), t, input)
	if err != nil {
		var validationErr *ValidationError
		var executionErr *ExecutionError
		switch {
		case errors.As(err, &validationErr):
			writeJSON(w, http.StatusUnprocessableEntity, wireError{
				Kind:        wireValidation,
				Tool:        validationErr.Tool,
				Description: validationErr.Description,
				Message:     validationErr.Err.Error(),
			})
		case errors.As(err, &executionErr):
			writeJSON(w, http.StatusInternalServerError, wireError{
				Kind:    wireExecution,
				Tool:    executionErr.Tool,
				Message: executionErr.Err.Error(),
			})
		default:
			writeJSON(w, http.StatusInternalServerError, wireError{
				Kind:    wireExecution,
				Tool:    name,
				Message: err.Error(),
			})
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(out)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
