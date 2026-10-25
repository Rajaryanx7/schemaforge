package schemaforge

import "errors"

type ActionType string

const (
	ActionInsert ActionType = "insert"
	ActionUpdate ActionType = "update"
	ActionDelete ActionType = "delete"
)

type Action struct {
	Type   ActionType     `json:"action"`
	Table  string         `json:"table"`
	ID     int64          `json:"id,omitempty"`
	Values map[string]any `json:"values,omitempty"`
}

func (a Action) Validate() error {
	if a.Table == "" {
		return errors.New("table is required")
	}

	switch a.Type {
	case ActionInsert:
		if len(a.Values) == 0 {
			return errors.New("insert requires values")
		}
	case ActionUpdate:
		if a.ID <= 0 || len(a.Values) == 0 {
			return errors.New("update requires a positive ID and values")
		}
	case ActionDelete:
		if a.ID <= 0 {
			return errors.New("delete requires a positive ID")
		}
	default:
		return errors.New("unsupported action")
	}

	return nil
}
