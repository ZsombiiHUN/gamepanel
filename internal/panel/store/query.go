package store

import (
	"context"
	"database/sql"
)

// querier is what *sql.DB and *sql.Tx have in common for reads.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// rowScanner is *sql.Row or *sql.Rows; the scan* helpers take it so they serve
// both single-row lookups and queryAll.
type rowScanner = interface{ Scan(dest ...any) error }

// count runs a SELECT COUNT(*)-style query that returns one integer.
func count(ctx context.Context, q querier, query string, args ...any) (int, error) {
	var n int
	if err := q.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// queryAll runs query and scans every row. The rows are closed before it
// returns: the writing connection is a single one, so a later query inside
// a transaction would wait on rows left open. A failure partway through returns the error, never
// a shortened list. An empty result is an empty, non-nil slice, which the API
// encodes as [] rather than null.
func queryAll[T any](ctx context.Context, q querier, scan func(rowScanner) (T, error), query string, args ...any) ([]T, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func scanInt(sc rowScanner) (int, error) {
	var n int
	err := sc.Scan(&n)
	return n, err
}
