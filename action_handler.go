package main

import (
	"context"
	"errors"
	"net/http"
	"time"

	sf "schemaforge/schemaforge"
)

type actionRequest struct {
	Instruction string `json:"instruction"`
}

func (s *server) handleAction(w http.ResponseWriter, r *http.Request) {
	var req actionRequest

	if apiErr := decodeJSON(w, r, &req, true); apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 100*time.Second)
	defer cancel()

	action, token, err := s.library.Preview(ctx, req.Instruction)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message":               "Action validated. No database changes were made.",
		"action":                action,
		"preview_token":         token,
		"requires_confirmation": true,
	})
}

type executeActionRequest struct {
	Action       sf.Action `json:"action"`
	PreviewToken string    `json:"preview_token"`
	Confirm      bool      `json:"confirm"`
}

func (s *server) handleExecuteAction(w http.ResponseWriter, r *http.Request) {
	var req executeActionRequest

	if apiErr := decodeJSON(w, r, &req, true); apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}

	if !req.Confirm {
		writeError(w, http.StatusBadRequest, "explicit confirmation is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	result, err := s.library.Execute(
		ctx,
		req.Action,
		req.PreviewToken,
		req.Confirm,
	)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		writeError(w, status, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "action executed successfully",
		"action":  req.Action,
		"result":  result,
	})
}
