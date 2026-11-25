package daemon

import (
	"errors"
	"io/fs"
	"strings"
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
	case errors.Is(err, fsx.ErrUnsupportedArchive):
		return proto.Invalid(fsx.ErrUnsupportedArchive.Error())
	case errors.Is(err, fsx.ErrArchiveTooLarge), errors.Is(err, fsx.ErrTooBig):
		return &proto.Error{Code: proto.CodeDiskFull, Msg: "that does not fit in the server's disk limit"}
	case errors.Is(err, syscall.EINVAL):
		return proto.Invalid("a folder cannot be moved or copied into itself")
	case strings.Contains(err.Error(), "io.weight"):
		// runc, on a kernel without the io controller's weight file.
		return proto.Invalid("this node cannot apply an IO weight; set the server's IO weight to 0 and start it again")
	case strings.Contains(err.Error(), "unsafe path in archive"):
		return proto.Invalid("the archive contains unsafe paths and was not extracted")
	case strings.Contains(err.Error(), "damaged") || strings.Contains(err.Error(), "not a gzip"):
		return proto.Invalid("the archive is damaged or not what its name says")
	}
	return err
}
