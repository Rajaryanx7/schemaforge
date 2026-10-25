package main

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type actionRequest struct {
	Instruction string `json:"instruction"`
	Confirm     bool   `json:"confirm"`
}

func (s *server) handleAction(w http.ResponseWriter, r *http.Request) {
	var req actionRequest

	if apiErr := decodeJSON(w, r, &req, true); apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 100*time.Second)
	defer cancel()

	action, err := parseAction(ctx, req.Instruction)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := validateTableName(action.Table); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	dbCtx, cancelDB := context.WithTimeout(r.Context(), dbTimeout)
	defer cancelDB()

	cols, err := s.loadColumns(dbCtx, action.Table)
	if err != nil {
		handleDBError(w, "validate action table", err)
		return
	}
	if len(cols) == 0 {
		writeError(w, http.StatusNotFound,
			"table does not exist: "+action.Table)
		return
	}

	columnMap := make(map[string]column, len(cols))
	for _, col := range cols {
		columnMap[col.Name] = col
	}

	switch action.Action {
	case ActionInsert, ActionUpdate:
		if len(action.Values) == 0 {
			writeError(w, http.StatusBadRequest, "action requires values")
			return
		}

		for name, value := range action.Values {
			col, exists := columnMap[name]
			if !exists {
				writeError(w, http.StatusBadRequest,
					fmt.Sprintf("unknown field %q for table %q", name, action.Table))
				return
			}
			if col.Identity {
				writeError(w, http.StatusBadRequest,
					fmt.Sprintf("field %q is generated automatically", name))
				return
			}
			if _, err := convertValue(col, value); err != nil {
				writeError(w, http.StatusBadRequest,
					fmt.Sprintf("invalid value for field %q: %v", name, err))
				return
			}
		}

	case ActionDelete:
		if action.ID <= 0 {
			writeError(w, http.StatusBadRequest, "delete requires a positive record ID")
			return
		}

	default:
		writeError(w, http.StatusBadRequest, "unsupported action")
		return
	}

	if action.Action == ActionUpdate || action.Action == ActionDelete {
		if _, hasID := columnMap["id"]; !hasID {
			writeError(w, http.StatusBadRequest,
				"this table does not have an id column")
			return
		}
		if action.ID <= 0 {
			writeError(w, http.StatusBadRequest,
				"update/delete requires a positive record ID")
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message":               "Action validated. No database changes were made.",
		"action":                action,
		"requires_confirmation": true,
	})
}

type executeActionRequest struct {
	Action  DatabaseAction `json:"action"`
	Confirm bool           `json:"confirm"`
}

func (s *server) handleExecuteAction(w http.ResponseWriter, r *http.Request) {
	var req executeActionRequest

	if apiErr := decodeJSON(w, r, &req, true); apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}

	if !req.Confirm {
		writeError(w, http.StatusBadRequest,
			"explicit confirmation is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	action := &req.Action

	if err := validateTableName(action.Table); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	cols, err := s.loadColumns(ctx, action.Table)
	if err != nil {
		handleDBError(w, "validate action", err)
		return
	}
	if len(cols) == 0 {
		writeError(w, http.StatusNotFound, "table does not exist")
		return
	}

	result, err := executeAction(ctx, s.db, action, cols)
	if err != nil {
		if err.Error() == "record not found" {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "action executed successfully",
		"action":  action,
		"result":  result,
	})
}
