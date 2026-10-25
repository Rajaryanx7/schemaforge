package schemaforge

import (
	"context"
	"errors"
	"fmt"
	"regexp"
)

type dbColumn struct {
	Name     string
	DataType string
	Identity bool
}

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

func validateIdentifier(name string) error {
	if !identifierPattern.MatchString(name) {
		return fmt.Errorf("invalid identifier %q", name)
	}

	return nil
}

func (c *Client) loadColumns(ctx context.Context, table string) ([]dbColumn, error) {
	const query = `
		SELECT
			c.column_name::text,
			c.data_type::text,
			(c.is_identity = 'YES')
		FROM information_schema.columns c
		JOIN information_schema.tables t
			ON t.table_schema = c.table_schema
			AND t.table_name = c.table_name
		WHERE c.table_schema = 'public'
			AND t.table_type = 'BASE TABLE'
			AND c.table_name::text = $1
		ORDER BY c.ordinal_position`

	rows, err := c.db.Query(ctx, query, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []dbColumn

	for rows.Next() {
		var col dbColumn

		if err := rows.Scan(
			&col.Name,
			&col.DataType,
			&col.Identity,
		); err != nil {
			return nil, err
		}

		cols = append(cols, col)
	}

	return cols, rows.Err()
}

func (c *Client) validateAction(ctx context.Context, action *Action) error {
	if action == nil {
		return errors.New("action is required")
	}

	if err := action.Validate(); err != nil {
		return err
	}

	if err := validateIdentifier(action.Table); err != nil {
		return err
	}

	cols, err := c.loadColumns(ctx, action.Table)
	if err != nil {
		return fmt.Errorf("load table columns: %w", err)
	}

	if len(cols) == 0 {
		return fmt.Errorf(
			"table %q does not exist in the public schema",
			action.Table,
		)
	}

	columnMap := make(map[string]dbColumn, len(cols))

	for _, col := range cols {
		columnMap[col.Name] = col
	}

	switch action.Type {
	case ActionInsert, ActionUpdate:
		if len(action.Values) == 0 {
			return errors.New("action requires values")
		}

		for name, value := range action.Values {
			if err := validateIdentifier(name); err != nil {
				return err
			}

			col, exists := columnMap[name]
			if !exists {
				return fmt.Errorf("column %q does not exist", name)
			}

			if col.Identity {
				return fmt.Errorf(
					"cannot set identity column %q",
					name,
				)
			}

			if _, err := convertActionValue(value, col.DataType); err != nil {
				return fmt.Errorf(
					"invalid value for column %q: %w",
					name,
					err,
				)
			}
		}

	case ActionDelete:
		// No column values need validation for a delete.

	default:
		return errors.New("unsupported action")
	}

	if action.Type == ActionUpdate || action.Type == ActionDelete {
		if action.ID <= 0 {
			return errors.New("a positive record ID is required")
		}

		if _, exists := columnMap["id"]; !exists {
			return errors.New("table must contain an id column")
		}
	}

	return nil
}
