package store

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

// Location groups nodes: a data centre or a region. Shops sell servers "in"
// a location, and the panel picks a node there.
type Location struct {
	ID        int64  `json:"id"`
	Short     string `json:"short" doc:"a short code, unique, e.g. eu-west"`
	Long      string `json:"long" doc:"a description, e.g. Frankfurt, Germany"`
	Nodes     int    `json:"nodes" doc:"nodes in this location"`
	CreatedAt int64  `json:"created_at"`
}

var (
	// ErrLocationTaken means another location has that short code.
	ErrLocationTaken = userErr("another location has that short code")
	// ErrLocationInUse means a location still has nodes.
	ErrLocationInUse = userErr("this location still has nodes; move them to another location first")
)

const locationCols = `id,short,long,(SELECT COUNT(*) FROM nodes WHERE location_id=locations.id),created_at`

func scanLocation(sc rowScanner) (Location, error) {
	var l Location
	err := sc.Scan(&l.ID, &l.Short, &l.Long, &l.Nodes, &l.CreatedAt)
	return l, nf(err)
}

// checkLocation trims and checks a location's codes.
func checkLocation(short, long string) (string, string, error) {
	short, long = strings.TrimSpace(short), strings.TrimSpace(long)
	switch {
	case short == "" || utf8.RuneCountInString(short) > 60:
		return "", "", userErr("the short code must be 1 to 60 characters")
	case strings.ContainsAny(short, "\r\n\t") || strings.ContainsAny(long, "\r\n\t"):
		return "", "", userErr("location names must be on one line")
	case utf8.RuneCountInString(long) > 191:
		return "", "", userErr("the description can be at most 191 characters")
	}
	return short, long, nil
}

// ListLocations returns every location, by id.
func (s *Store) ListLocations(ctx context.Context) ([]Location, error) {
	return queryAll(ctx, s.RO, scanLocation, `SELECT `+locationCols+` FROM locations ORDER BY id`)
}

// GetLocation loads one location.
func (s *Store) GetLocation(ctx context.Context, id int64) (Location, error) {
	return scanLocation(s.RO.QueryRowContext(ctx, `SELECT `+locationCols+` FROM locations WHERE id=?`, id))
}

// CreateLocation adds a location.
func (s *Store) CreateLocation(ctx context.Context, short, long string) (Location, error) {
	short, long, err := checkLocation(short, long)
	if err != nil {
		return Location{}, err
	}
	res, err := s.exec(ctx, `INSERT INTO locations(short,long,created_at) VALUES(?,?,?)`, short, long, now())
	if err != nil {
		return Location{}, locationUniqueErr(err)
	}
	id, _ := res.LastInsertId()
	return s.GetLocation(ctx, id)
}

// UpdateLocation changes a location's codes; nil leaves one as it is.
func (s *Store) UpdateLocation(ctx context.Context, id int64, short, long *string) (Location, error) {
	cur, err := s.GetLocation(ctx, id)
	if err != nil {
		return cur, err
	}
	if short != nil {
		cur.Short = *short
	}
	if long != nil {
		cur.Long = *long
	}
	sh, lo, err := checkLocation(cur.Short, cur.Long)
	if err != nil {
		return cur, err
	}
	if _, err := s.exec(ctx, `UPDATE locations SET short=?, long=? WHERE id=?`, sh, lo, id); err != nil {
		return cur, locationUniqueErr(err)
	}
	return s.GetLocation(ctx, id)
}

// DeleteLocation removes a location that has no nodes.
func (s *Store) DeleteLocation(ctx context.Context, id int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	n, err := count(ctx, tx, `SELECT COUNT(*) FROM nodes WHERE location_id=?`, id)
	if err != nil {
		return err
	}
	if n > 0 {
		return ErrLocationInUse
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM locations WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// SetNodeLocation puts a node in a location; 0 takes it out of any.
func (s *Store) SetNodeLocation(ctx context.Context, nodeID, locationID int64) error {
	if locationID != 0 {
		if _, err := s.GetLocation(ctx, locationID); errors.Is(err, ErrNotFound) {
			return userErr("there is no location with that id")
		} else if err != nil {
			return err
		}
	}
	var loc any
	if locationID != 0 {
		loc = locationID
	}
	res, err := s.exec(ctx, `UPDATE nodes SET location_id=? WHERE id=?`, loc, nodeID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func locationUniqueErr(err error) error {
	if err != nil && strings.Contains(err.Error(), "locations.short") {
		return ErrLocationTaken
	}
	return err
}
