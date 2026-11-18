package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/panel/store"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

type taskIn struct {
	Action            string `json:"action" enum:"command,power,backup"`
	Payload           string `json:"payload,omitempty" maxLength:"2000"`
	OffsetSeconds     int    `json:"offset_seconds,omitempty" minimum:"0" maximum:"900"`
	ContinueOnFailure bool   `json:"continue_on_failure,omitempty"`
}
type scheduleBody struct {
	Name           string   `json:"name" minLength:"1" maxLength:"64"`
	Cron           string   `json:"cron" minLength:"9" maxLength:"100" doc:"5 fields (UTC): minute hour day-of-month month day-of-week"`
	Enabled        bool     `json:"enabled"`
	OnlyWhenOnline bool     `json:"only_when_online,omitempty" doc:"skip runs while the server is not running"`
	Tasks          []taskIn `json:"tasks" maxItems:"20"`
}

// toSchedule builds the schedule as saved by author, whose permissions its
// tasks will run with.
func (b scheduleBody) toSchedule(server string, id int64, author store.User) store.Schedule {
	v := store.Schedule{ID: id, ServerUUID: server, Name: b.Name, Cron: b.Cron, Enabled: b.Enabled, OnlyWhenOnline: b.OnlyWhenOnline, CreatedBy: author.ID}
	for _, t := range b.Tasks {
		v.Tasks = append(v.Tasks, store.Task{Action: t.Action, Payload: t.Payload, OffsetSeconds: t.OffsetSeconds, ContinueOnFailure: t.ContinueOnFailure})
	}
	return v
}

type scheduleCreateIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Body scheduleBody
}
type scheduleIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	ID   int64  `path:"id"`
}
type scheduleUpdateIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	ID   int64  `path:"id"`
	Body scheduleBody
}
type scheduleOut struct{ Body store.Schedule }
type scheduleListOut struct{ Body []store.Schedule }

func (a *App) registerSchedules(api huma.API) {
	huma.Register(api, huma.Operation{OperationID: "listSchedules", Method: "GET", Path: "/servers/{uuid}/schedules", Summary: "List schedules"},
		func(ctx context.Context, in *uuidIn) (*scheduleListOut, error) {
			_, s, err := a.authorizeP(ctx, in.UUID, PermSchedulesManage)
			if err != nil {
				return nil, err
			}
			l, err := a.Store.ListSchedules(ctx, s.UUID)
			if err != nil {
				return nil, huma.Error500InternalServerError("db error")
			}
			return &scheduleListOut{Body: l}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "createSchedule", Method: "POST", Path: "/servers/{uuid}/schedules", Summary: "Create a schedule", DefaultStatus: 201},
		func(ctx context.Context, in *scheduleCreateIn) (*scheduleOut, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermSchedulesManage)
			if err != nil {
				return nil, err
			}
			if err := a.checkTaskPerms(ctx, u, s, in.Body); err != nil {
				return nil, err
			}
			v, err := a.Store.SaveSchedule(ctx, in.Body.toSchedule(s.UUID, 0, u))
			if err != nil {
				return nil, userErr(http.StatusUnprocessableEntity, err)
			}
			a.audit(ctx, u, "schedule.create", s.UUID, v.Name)
			return &scheduleOut{Body: v}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "updateSchedule", Method: "PUT", Path: "/servers/{uuid}/schedules/{id}", Summary: "Replace a schedule and its tasks"},
		func(ctx context.Context, in *scheduleUpdateIn) (*scheduleOut, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermSchedulesManage)
			if err != nil {
				return nil, err
			}
			if err := a.checkTaskPerms(ctx, u, s, in.Body); err != nil {
				return nil, err
			}
			v, err := a.Store.SaveSchedule(ctx, in.Body.toSchedule(s.UUID, in.ID, u))
			if errors.Is(err, store.ErrNotFound) {
				return nil, huma.Error404NotFound("schedule not found")
			}
			if err != nil {
				return nil, userErr(http.StatusUnprocessableEntity, err)
			}
			a.audit(ctx, u, "schedule.update", s.UUID, v.Name)
			return &scheduleOut{Body: v}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "deleteSchedule", Method: "DELETE", Path: "/servers/{uuid}/schedules/{id}", Summary: "Delete a schedule", DefaultStatus: 204},
		func(ctx context.Context, in *scheduleIn) (*struct{}, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermSchedulesManage)
			if err != nil {
				return nil, err
			}
			if err := a.Store.DeleteSchedule(ctx, s.UUID, in.ID); err != nil {
				return nil, huma.Error404NotFound("schedule not found")
			}
			a.audit(ctx, u, "schedule.delete", s.UUID, fmt.Sprint(in.ID))
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "runSchedule", Method: "POST", Path: "/servers/{uuid}/schedules/{id}/run",
		Summary: "Run a schedule's tasks now", DefaultStatus: 202},
		func(ctx context.Context, in *scheduleIn) (*struct{}, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermSchedulesManage)
			if err != nil {
				return nil, err
			}
			v, err := a.Store.GetSchedule(ctx, s.UUID, in.ID)
			if err != nil {
				return nil, huma.Error404NotFound("schedule not found")
			}
			if !a.beginRun(v.ID) {
				return nil, huma.Error409Conflict("this schedule is already running")
			}
			a.audit(ctx, u, "schedule.run", s.UUID, v.Name)
			a.Store.TouchSchedule(ctx, v.ID)
			go a.runSchedule(context.Background(), s, v, true)
			return nil, nil
		})
}

// ---- scheduler ----

func (a *App) beginRun(id int64) bool {
	_, loaded := a.running.LoadOrStore(id, struct{}{})
	return !loaded
}

// RunScheduler fires due schedules until ctx ends. Each tick claims due
// schedules atomically in the database, so a firing runs exactly once.
func (a *App) RunScheduler(ctx context.Context, tick time.Duration) {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		due, err := a.Store.ClaimDue(ctx, time.Now())
		if err != nil {
			a.Log.Warn("scheduler: claim failed", "err", err)
			continue
		}
		for _, v := range due {
			s, err := a.Store.GetServer(ctx, v.ServerUUID)
			if err != nil || !a.beginRun(v.ID) {
				continue // server gone, or the previous run is still going
			}
			go a.runSchedule(ctx, s, v, false)
		}
	}
}

// runSchedule executes the tasks in order. A failed task stops the run unless
// it is marked continue_on_failure. manual runs ignore only_when_online.
func (a *App) runSchedule(ctx context.Context, s store.Server, v store.Schedule, manual bool) {
	defer a.running.Delete(v.ID)
	if s.Suspended {
		a.Store.SetScheduleStatus(ctx, v.ID, "skipped: the server is suspended")
		return
	}
	if !manual && v.OnlyWhenOnline && a.Hub.State(s.UUID) != "running" {
		a.Store.SetScheduleStatus(ctx, v.ID, "skipped: server was not running")
		return
	}
	if err := a.authorPermits(ctx, s, v); err != nil {
		a.Store.SetScheduleStatus(ctx, v.ID, "skipped: "+failureText(err))
		return
	}
	status := "ok"
	for i, t := range v.Tasks {
		if t.OffsetSeconds > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(t.OffsetSeconds) * time.Second):
			}
		}
		if err := a.runTask(ctx, s, t); err != nil {
			status = fmt.Sprintf("task %d (%s) failed: %s", i+1, t.Action, failureText(err))
			a.Log.Warn("scheduled task failed", "server", s.UUID, "schedule", v.ID, "task", i+1, "err", err)
			if !t.ContinueOnFailure {
				break
			}
		}
	}
	a.Store.SetScheduleStatus(ctx, v.ID, time.Now().UTC().Format("2006-01-02 15:04 UTC")+" "+status)
}

func (a *App) runTask(ctx context.Context, s store.Server, t store.Task) error {
	switch t.Action {
	case store.ActionCommand:
		_, err := a.Hub.Request(ctx, s.NodeID, proto.ReqServerInput, proto.Input{UUID: s.UUID, Line: t.Payload})
		return err
	case store.ActionPower:
		_, err := a.Hub.Request(ctx, s.NodeID, proto.ReqServerPower, proto.Power{UUID: s.UUID, Action: t.Payload})
		return err
	case store.ActionBackup:
		_, done, err := a.startBackup(s, "", t.Payload, true)
		if err != nil {
			return err
		}
		return <-done
	}
	return fmt.Errorf("unknown action %q", t.Action)
}

// authorPermits checks, at run time, that whoever saved the schedule may still
// do every task in it. Permissions change after a schedule is saved (a
// subuser is removed or loses "power"); the schedule must not keep doing what
// its author no longer could. Schedules saved before authors were recorded
// run with the owner's permissions.
func (a *App) authorPermits(ctx context.Context, s store.Server, v store.Schedule) error {
	if v.CreatedBy == 0 {
		return nil
	}
	u, err := a.Store.GetUser(ctx, v.CreatedBy)
	if err != nil {
		return &store.UserError{Msg: "the account that saved this schedule no longer exists"}
	}
	perms, _, ok := a.access(ctx, u, s)
	if !ok {
		return &store.UserError{Msg: u.Email + " no longer has access to this server"}
	}
	for _, t := range v.Tasks {
		if p := taskPerm(t.Action); !has(perms, p) {
			return &store.UserError{Msg: u.Email + " no longer has the " + p + " permission"}
		}
	}
	return nil
}

// checkTaskPerms refuses tasks the caller could not perform directly.
func (a *App) checkTaskPerms(ctx context.Context, u store.User, s store.Server, b scheduleBody) error {
	perms, _, _ := a.access(ctx, u, s)
	for _, t := range b.Tasks {
		if p := taskPerm(t.Action); !has(perms, p) {
			return huma.Error403Forbidden("you cannot schedule a " + t.Action + " task without the " + p + " permission")
		}
	}
	return nil
}
