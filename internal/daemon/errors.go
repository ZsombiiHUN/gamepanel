package daemon

import (
	"errors"
	"io/fs"
	"syscall"

	"github.com/ZsombiiHUN/gamepanel/internal/daemon/fsx"
	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

// forPeople gives common uncoded failures a code and a plain message, so the
// panel can show them. File system errors otherwise read like
// "openat world/level.dat: no such file or directory". Anything left uncoded
// is shown by the panel only as a generic failure (the daemon logs the detail).
func forPeople(err error) error {
	if proto.CodeOf(err) != "" {
		return err
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return proto.NotFound("no such file or folder")
	case errors.Is(err, fs.ErrExist):
		return proto.Conflict("a file or folder with that name already exists")
	case errors.Is(err, syscall.EISDIR):
		return proto.Invalid("that is a folder, not a file")
	case errors.Is(err, syscall.ENOTDIR):
		return proto.Invalid("part of that path is a file, not a folder")
	case errors.Is(err, syscall.ENOTEMPTY):
		return proto.Conflict("that folder is not empty")
	case errors.Is(err, syscall.ENAMETOOLONG):
		return proto.Invalid("that name is too long")
	case errors.Is(err, fsx.ErrTooLarge):
		return proto.Invalid(fsx.ErrTooLarge.Error())
	}
	return err
}
