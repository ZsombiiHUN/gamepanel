package daemon

// Requests from the panel: decoding, panic safety, and file requests.

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/fsx"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

func (d *Daemon) handle(ctx context.Context, env proto.Envelope) {
	data, err := d.safeDispatch(ctx, env)
	reply := proto.Envelope{Type: proto.EvtReply, ID: env.ID, Data: data}
	if err != nil {
		d.log.Warn("request failed", "type", env.Type, "err", err)
		err = forPeople(err)
		reply.Err, reply.Code = err.Error(), proto.CodeOf(err)
	}
	_ = d.send(ctx, reply)
}

// safeDispatch is dispatch with a recover: requests run in their own
// goroutines, so a panic in one would otherwise take down the whole daemon and
// every server's console with it.
func (d *Daemon) safeDispatch(ctx context.Context, env proto.Envelope) (data json.RawMessage, err error) {
	defer func() {
		if r := recover(); r != nil {
			d.log.Error("request panicked", "type", env.Type, "panic", r, "stack", string(debug.Stack()))
			data, err = nil, fmt.Errorf("internal error handling %s", env.Type)
		}
	}()
	return d.dispatch(ctx, env)
}

func (d *Daemon) dir(uuid string) (*fsx.Dir, error) {
	if !uuidRe.MatchString(uuid) {
		return nil, proto.Invalid("invalid server id")
	}
	return fsx.Open(d.serverDir(uuid))
}

func (d *Daemon) dispatch(ctx context.Context, env proto.Envelope) (json.RawMessage, error) {
	switch env.Type {
	case proto.ReqServerCreate:
		var s proto.ServerSpec
		if err := json.Unmarshal(env.Data, &s); err != nil {
			return nil, err
		}
		return nil, d.create(ctx, s)
	case proto.ReqServerPower:
		var p proto.Power
		if err := json.Unmarshal(env.Data, &p); err != nil {
			return nil, err
		}
		return nil, d.power(ctx, p)
	case proto.ReqServerUpdate:
		var sp proto.ServerSpec
		if err := json.Unmarshal(env.Data, &sp); err != nil {
			return nil, err
		}
		res, err := d.update(ctx, sp)
		return proto.Marshal(res), err
	case proto.ReqServerReinstall:
		var u proto.UUIDOnly
		if err := json.Unmarshal(env.Data, &u); err != nil {
			return nil, err
		}
		return nil, d.reinstall(ctx, u.UUID)
	case proto.ReqBackupCreate, proto.ReqBackupRestore, proto.ReqBackupDelete:
		var b proto.BackupReq
		if err := json.Unmarshal(env.Data, &b); err != nil {
			return nil, err
		}
		return d.backup(ctx, env.Type, b)
	case proto.ReqServerInput:
		var in proto.Input
		if err := json.Unmarshal(env.Data, &in); err != nil {
			return nil, err
		}
		return nil, d.input(in.UUID, in.Line)
	case proto.ReqServerPing:
		var u proto.UUIDOnly
		if err := json.Unmarshal(env.Data, &u); err != nil {
			return nil, err
		}
		st, err := d.ping(ctx, u.UUID)
		if err != nil {
			return nil, err
		}
		return proto.Marshal(st), nil
	case proto.ReqServerDelete:
		var u proto.UUIDOnly
		if err := json.Unmarshal(env.Data, &u); err != nil {
			return nil, err
		}
		return nil, d.remove(ctx, u.UUID)
	case proto.ReqFilesList, proto.ReqFilesRead, proto.ReqFilesWrite, proto.ReqFilesDelete, proto.ReqFilesMkdir:
		var f proto.FileReq
		if err := json.Unmarshal(env.Data, &f); err != nil {
			return nil, err
		}
		return d.files(env.Type, f)
	case proto.ReqFilesOp:
		var op proto.FileOp
		if err := json.Unmarshal(env.Data, &op); err != nil {
			return nil, err
		}
		res, err := d.fileOp(op)
		return proto.Marshal(res), err
	case proto.ReqFilesUpload, proto.ReqFilesDownload, proto.ReqFilesPull, proto.ReqBackupDownload:
		return d.transfer(ctx, env.Type, env.Data)
	}
	return nil, fmt.Errorf("unknown request %q", env.Type)
}

func (d *Daemon) files(typ string, f proto.FileReq) (json.RawMessage, error) {
	sd, err := d.dir(f.UUID)
	if err != nil {
		return nil, err
	}
	defer sd.Close()
	switch typ {
	case proto.ReqFilesList:
		l, err := sd.List(f.Path)
		return proto.Marshal(l), err
	case proto.ReqFilesRead:
		b, err := sd.Read(f.Path)
		return proto.Marshal(proto.FileReq{UUID: f.UUID, Path: f.Path, Content: b}), err
	case proto.ReqFilesWrite:
		if err := d.reserveDisk(f.UUID, int64(len(f.Content))); err != nil {
			return nil, err
		}
		return nil, sd.Write(f.Path, f.Content)
	case proto.ReqFilesMkdir:
		return nil, sd.Mkdir(f.Path)
	default:
		return nil, sd.Delete(f.Path)
	}
}
