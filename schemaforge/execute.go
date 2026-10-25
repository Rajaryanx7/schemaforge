package schemaforge

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Execute runs an action that matches a valid, unused preview token.
//
// The action must match the action returned by Preview, the preview token
// must not be expired or previously used, and confirm must be true.
//
// The preview token is consumed before database revalidation and execution.
// Therefore, a failed execution may require creating a new preview.
//
// Database values are passed as query parameters, and identifiers are quoted
// before being included in SQL statements.
func (c *Client) Execute(
	ctx context.Context,
	action Action,
	previewToken string,
	confirm bool,
) (map[string]any, error) {
	if c == nil || c.db == nil {
		return nil, errors.New("SchemaForge client is not initialized")
	}

	if !confirm {
		return nil, errors.New("explicit confirmation is required")
	}

	if err := action.Validate(); err != nil {
		return nil, err
	}

	if err := c.consumePreview(previewToken, action); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	if err := c.validateAction(ctx, &action); err != nil {
		return nil, err
	}

	return executeAction(ctx, c, &action)
}

func executeAction(ctx context.Context, c *Client, action *Action) (map[string]any, error) {
	cols, err := c.loadColumns(ctx, action.Table)
	if err != nil {
		return nil, err
	}

	columnMap := make(map[string]dbColumn, len(cols))
	for _, col := range cols {
		columnMap[col.Name] = col
	}

	switch action.Type {
	case ActionInsert:
		names := make([]string, 0, len(action.Values))
		for name := range action.Values {
			names = append(names, name)
		}
		sort.Strings(names)

		placeholders := make([]string, len(names))
		args := make([]any, len(names))
		for i, name := range names {
			col := columnMap[name]
			value, err := convertActionValue(action.Values[name], col.DataType)
			if err != nil {
				return nil, fmt.Errorf("invalid value for %q: %w", name, err)
			}
			placeholders[i] = fmt.Sprintf("$%d", i+1)
			args[i] = value
		}

		query := fmt.Sprintf(
			"INSERT INTO public.%s (%s) VALUES (%s) RETURNING *",
			quoteIdent(action.Table),
			quotedColumns(names),
			strings.Join(placeholders, ", "),
		)

		return queryOne(ctx, c, query, args...)

	case ActionUpdate:
		names := make([]string, 0, len(action.Values))
		for name := range action.Values {
			names = append(names, name)
		}
		sort.Strings(names)

		assignments := make([]string, len(names))
		args := make([]any, 0, len(names)+1)

		for i, name := range names {
			col := columnMap[name]
			value, err := convertActionValue(action.Values[name], col.DataType)
			if err != nil {
				return nil, fmt.Errorf("invalid value for %q: %w", name, err)
			}
			assignments[i] = fmt.Sprintf("%s = $%d", quoteIdent(name), i+1)
			args = append(args, value)
		}

		args = append(args, action.ID)
		query := fmt.Sprintf(
			"UPDATE public.%s SET %s WHERE id = $%d RETURNING *",
			quoteIdent(action.Table),
			strings.Join(assignments, ", "),
			len(args),
		)

		return queryOne(ctx, c, query, args...)

	case ActionDelete:
		query := fmt.Sprintf(
			"DELETE FROM public.%s WHERE id = $1 RETURNING id",
			quoteIdent(action.Table),
		)

		return queryOne(ctx, c, query, action.ID)

	default:
		return nil, errors.New("unsupported action")
	}
}

func quotedColumns(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = quoteIdent(name)
	}
	return strings.Join(quoted, ", ")
}

func queryOne(ctx context.Context, c *Client, query string, args ...any) (map[string]any, error) {
	rows, err := c.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	row, err := pgx.CollectOneRow(rows, pgx.RowToMap)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("record not found")
	}
	if err != nil {
		return nil, err
	}

	return row, nil
}
