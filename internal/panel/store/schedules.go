package store

import (
	"context"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

const (
	ActionCommand = "command"
	ActionPower   = "power"
	ActionBackup  = "backup"

	MaxTasksPerSchedule   = 20
	MaxSchedulesPerServer = 20
	MaxTaskOffsetSeconds  = 900
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// NextRun parses a 5-field cron expression (UTC) and returns its next fire
// time after from. Descriptors like @daily are not accepted.
func NextRun(expr string, from time.Time) (time.Time, error) {
	expr = strings.TrimSpace(expr)
	if strings.HasPrefix(expr, "@") || len(strings.Fields(expr)) != 5 {
		return time.Time{}, userErr("cron needs 5 fields: minute hour day-of-month month day-of-week")
	}
	sc, err := cronParser.Parse(expr)
	if err != nil {
		return time.Time{}, userErrf("invalid cron expression: %v", err)
	}
	next := sc.Next(from.UTC())
	if next.IsZero() {
		return time.Time{}, userErr("cron expression never fires")
	}
	return next, nil
}

type Task struct {
	ID                int64  `json:"id"`
	Seq               int    `json:"seq"`
	Action            string `json:"action" enum:"command,power,backup"`
	Payload           string `json:"payload" doc:"command text, power action (start|stop|restart|kill), or backup ignore patterns"`
	OffsetSeconds     int    `json:"offset_seconds" minimum:"0" maximum:"900"`
	ContinueOnFailure bool   `json:"continue_on_failure"`
}

type Schedule struct {
	ID             int64  `json:"id"`
	ServerUUID     string `json:"server_uuid"`
	Name           string `json:"name"`
	Cron           string `json:"cron"`
	Enabled        bool   `json:"enabled"`
	OnlyWhenOnline bool   `json:"only_when_online"`
	NextRun        int64  `json:"next_run" doc:"unix seconds, 0 = not scheduled"`
	LastRun        int64  `json:"last_run"`
	LastStatus     string `json:"last_status"`
	Tasks          []Task `json:"tasks" nullable:"false"`
	// CreatedBy is who last saved the schedule; its tasks run with that
	// account's permissions. 0 = saved before this was recorded (runs as owner).
	CreatedBy int64 `json:"created_by"`
}

// ValidateTask checks a task before it is stored.
func ValidateTask(t Task) error {
	switch t.Action {
	case ActionCommand:
		if strings.TrimSpace(t.Payload) == "" || len(t.Payload) > 1000 || strings.ContainsAny(t.Payload, "\r\n") {
			return userErr("command must be a single line of 1 to 1000 characters")
		}
	case ActionPower:
		switch t.Payload {
		case "start", "stop", "restart", "kill":
		default:
			return userErr("power action must be start, stop, restart or kill")
		}
	case ActionBackup:
		if len(t.Payload) > 2000 {
			return userErr("ignore patterns too long")
		}
	default:
		return userErr("action must be command, power or backup")
	}
	if t.OffsetSeconds < 0 || t.OffsetSeconds > MaxTaskOffsetSeconds {
		return userErrf("offset must be 0 to %d seconds", MaxTaskOffsetSeconds)
	}
	return nil
}

func (s *Store) tasks(ctx context.Context, scheduleID int64) ([]Task, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,seq,action,payload,offset_seconds,continue_on_failure FROM schedule_tasks WHERE schedule_id=? ORDER BY seq`, scheduleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.Seq, &t.Action, &t.Payload, &t.OffsetSeconds, &t.ContinueOnFailure); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

const schedCols = `id,server_uuid,name,cron,enabled,only_when_online,next_run,last_run,last_status,COALESCE(created_by,0)`

func scanSchedule(sc interface{ Scan(...any) error }) (Schedule, error) {
	var v Schedule
	if err := sc.Scan(&v.ID, &v.ServerUUID, &v.Name, &v.Cron, &v.Enabled, &v.OnlyWhenOnline, &v.NextRun, &v.LastRun, &v.LastStatus, &v.CreatedBy); err != nil {
		return v, nf(err)
	}
	return v, nil
}

func (s *Store) loadTasks(ctx context.Context, list []Schedule) ([]Schedule, error) {
	for i := range list {
		t, err := s.tasks(ctx, list[i].ID)
		if err != nil {
			return nil, err
		}
		list[i].Tasks = t
	}
	return list, nil
}

func (s *Store) ListSchedules(ctx context.Context, server string) ([]Schedule, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+schedCols+` FROM schedules WHERE server_uuid=? ORDER BY id`, server)
	if err != nil {
		return nil, err
	}
	var out []Schedule
	for rows.Next() {
		v, err := scanSchedule(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, v)
	}
	rows.Close()
	if out == nil {
		out = []Schedule{}
	}
	return s.loadTasks(ctx, out)
}

func (s *Store) GetSchedule(ctx context.Context, server string, id int64) (Schedule, error) {
	v, err := scanSchedule(s.DB.QueryRowContext(ctx, `SELECT `+schedCols+` FROM schedules WHERE id=? AND server_uuid=?`, id, server))
	if err != nil {
		return v, err
	}
	v.Tasks, err = s.tasks(ctx, v.ID)
	return v, err
}

// SaveSchedule creates (ID==0) or replaces a schedule and its task list.
func (s *Store) SaveSchedule(ctx context.Context, v Schedule) (Schedule, error) {
	next, err := NextRun(v.Cron, time.Now())
	if err != nil {
		return v, err
	}
	if len(v.Tasks) > MaxTasksPerSchedule {
		return v, userErrf("at most %d tasks per schedule", MaxTasksPerSchedule)
	}
	for _, t := range v.Tasks {
		if err := ValidateTask(t); err != nil {
			return v, err
		}
	}
	v.NextRun = 0
	if v.Enabled {
		v.NextRun = next.Unix()
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return v, err
	}
	defer tx.Rollback()
	if v.ID == 0 {
		var n int
		tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM schedules WHERE server_uuid=?`, v.ServerUUID).Scan(&n)
		if n >= MaxSchedulesPerServer {
			return v, userErrf("at most %d schedules per server", MaxSchedulesPerServer)
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO schedules(server_uuid,name,cron,enabled,only_when_online,next_run,created_by) VALUES(?,?,?,?,?,?,?)`,
			v.ServerUUID, v.Name, strings.TrimSpace(v.Cron), v.Enabled, v.OnlyWhenOnline, v.NextRun, nullID(v.CreatedBy))
		if err != nil {
			return v, err
		}
		v.ID, _ = res.LastInsertId()
	} else {
		res, err := tx.ExecContext(ctx, `UPDATE schedules SET name=?,cron=?,enabled=?,only_when_online=?,next_run=?,created_by=? WHERE id=? AND server_uuid=?`,
			v.Name, strings.TrimSpace(v.Cron), v.Enabled, v.OnlyWhenOnline, v.NextRun, nullID(v.CreatedBy), v.ID, v.ServerUUID)
		if err != nil {
			return v, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return v, ErrNotFound
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM schedule_tasks WHERE schedule_id=?`, v.ID); err != nil {
			return v, err
		}
	}
	for i, t := range v.Tasks {
		if _, err := tx.ExecContext(ctx, `INSERT INTO schedule_tasks(schedule_id,seq,action,payload,offset_seconds,continue_on_failure) VALUES(?,?,?,?,?,?)`,
			v.ID, i, t.Action, t.Payload, t.OffsetSeconds, t.ContinueOnFailure); err != nil {
			return v, err
		}
	}
	if err := tx.Commit(); err != nil {
		return v, err
	}
	return s.GetSchedule(ctx, v.ServerUUID, v.ID)
}

// nullID stores 0 as NULL, for optional references.
func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func (s *Store) DeleteSchedule(ctx context.Context, server string, id int64) error {
	res, err := s.exec(ctx, `DELETE FROM schedules WHERE id=? AND server_uuid=?`, id, server)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ClaimDue atomically takes every enabled schedule due at or before now and
// advances its next_run, so each firing is claimed by exactly one caller and a
// panel that was down runs a missed schedule once, not once per missed slot.
func (s *Store) ClaimDue(ctx context.Context, at time.Time) ([]Schedule, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+schedCols+` FROM schedules WHERE enabled=1 AND next_run>0 AND next_run<=?`, at.Unix())
	if err != nil {
		return nil, err
	}
	var due []Schedule
	for rows.Next() {
		v, err := scanSchedule(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		due = append(due, v)
	}
	rows.Close()
	var claimed []Schedule
	for _, v := range due {
		next := int64(0)
		if n, err := NextRun(v.Cron, at); err == nil {
			next = n.Unix()
		}
		res, err := s.exec(ctx, `UPDATE schedules SET next_run=?,last_run=? WHERE id=? AND next_run=?`, next, at.Unix(), v.ID, v.NextRun)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			claimed = append(claimed, v)
		}
	}
	return s.loadTasks(ctx, claimed)
}

func (s *Store) SetScheduleStatus(ctx context.Context, id int64, status string) {
	if len(status) > 300 {
		status = status[:300]
	}
	s.exec(ctx, `UPDATE schedules SET last_status=? WHERE id=?`, status, id)
}

// TouchSchedule records that a schedule ran just now (manual runs; timer runs
// are stamped when they are claimed).
func (s *Store) TouchSchedule(ctx context.Context, id int64) {
	s.exec(ctx, `UPDATE schedules SET last_run=? WHERE id=?`, now(), id)
}
