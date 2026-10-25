package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func executeAction(
	ctx context.Context,
	db *pgxpool.Pool,
	action *DatabaseAction,
	cols []column,
) (map[string]any, error) {
	if action == nil {
		return nil, errors.New("action is required")
	}

	if err := validateTableName(action.Table); err != nil {
		return nil, err
	}

	columnMap := make(map[string]column, len(cols))
	for _, col := range cols {
		columnMap[col.Name] = col
	}

	switch action.Action {
	case ActionInsert, ActionUpdate:
		if len(action.Values) == 0 {
			return nil, errors.New("values are required")
		}

		for name, value := range action.Values {
			col, ok := columnMap[name]
			if !ok {
				return nil, fmt.Errorf("unknown field %q", name)
			}
			if col.Identity {
				return nil, fmt.Errorf("cannot set generated field %q", name)
			}
			if _, err := convertValue(col, value); err != nil {
				return nil, fmt.Errorf("invalid value for %q: %w", name, err)
			}
		}

	case ActionDelete:
		if action.ID <= 0 {
			return nil, errors.New("a positive record ID is required")
		}

	default:
		return nil, errors.New("unsupported action")
	}

	fields := make([]string, 0, len(action.Values))
	for name := range action.Values {
		fields = append(fields, name)
	}
	sort.Strings(fields)

	switch action.Action {
	case ActionInsert:
		var names, placeholders []string
		var args []any

		for i, name := range fields {
			value, err := convertValue(columnMap[name], action.Values[name])
			if err != nil {
				return nil, err
			}
			names = append(names, quoteIdent(name))
			placeholders = append(placeholders, "$"+strconv.Itoa(i+1))
			args = append(args, value)
		}

		query := fmt.Sprintf(
			"INSERT INTO %s (%s) VALUES (%s) RETURNING *",
			qualifiedTable(action.Table),
			strings.Join(names, ", "),
			strings.Join(placeholders, ", "),
		)

		rows, err := db.Query(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		return pgx.CollectOneRow(rows, pgx.RowToMap)

	case ActionUpdate:
		if _, ok := columnMap["id"]; !ok {
			return nil, errors.New("table has no id column")
		}
		if action.ID <= 0 {
			return nil, errors.New("a positive record ID is required")
		}

		var sets []string
		var args []any

		for _, name := range fields {
			value, err := convertValue(columnMap[name], action.Values[name])
			if err != nil {
				return nil, err
			}
			args = append(args, value)
			sets = append(sets, quoteIdent(name)+" = $"+strconv.Itoa(len(args)))
		}

		args = append(args, action.ID)
		query := fmt.Sprintf(
			"UPDATE %s SET %s WHERE %s = $%d RETURNING *",
			qualifiedTable(action.Table),
			strings.Join(sets, ", "),
			quoteIdent("id"),
			len(args),
		)

		rows, err := db.Query(ctx, query, args...)
		if err != nil {
			return nil, err
		}
		return pgx.CollectOneRow(rows, pgx.RowToMap)

	case ActionDelete:
		if _, ok := columnMap["id"]; !ok {
			return nil, errors.New("table has no id column")
		}

		var id int64
		err := db.QueryRow(
			ctx,
			fmt.Sprintf(
				"DELETE FROM %s WHERE %s = $1 RETURNING %s",
				qualifiedTable(action.Table),
				quoteIdent("id"),
				quoteIdent("id"),
			),
			action.ID,
		).Scan(&id)

		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("record not found")
		}
		if err != nil {
			return nil, err
		}

		return map[string]any{"id": id}, nil
	}

	return nil, errors.New("unsupported action")
}
