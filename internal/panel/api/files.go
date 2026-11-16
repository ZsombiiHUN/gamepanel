package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ZsombiiHUN/gamepanel/internal/proto"
)

type pathOut struct {
	Body struct {
		Path string `json:"path" doc:"what was created"`
	}
}

type fileMoveIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Body struct {
		Moves []proto.Move `json:"moves" minItems:"1" maxItems:"1000" doc:"rename or move; parent folders are created"`
	}
}
type fileCopyIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Body struct {
		Path string `json:"path" minLength:"1" maxLength:"4096" doc:"copied next to itself as \"<name> copy\""`
	}
}
type fileChmodIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Body struct {
		Paths []string `json:"paths" minItems:"1" maxItems:"1000"`
		Mode  string   `json:"mode" pattern:"^[0-7]{3}$" doc:"octal permission bits, e.g. 644"`
	}
}
type fileDeleteIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Body struct {
		Paths []string `json:"paths" minItems:"1" maxItems:"1000"`
	}
}
type fileCompressIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Body struct {
		Root   string   `json:"root" maxLength:"4096" doc:"the folder the archive is made in; paths must be inside it"`
		Paths  []string `json:"paths" minItems:"1" maxItems:"1000"`
		Format string   `json:"format,omitempty" enum:"tar.gz,zip" default:"tar.gz"`
	}
}
type fileDecompressIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Body struct {
		Path string `json:"path" minLength:"1" maxLength:"4096" doc:".zip, .tar, .tar.gz, .tgz, .tar.bz2 or .gz"`
		Root string `json:"root,omitempty" maxLength:"4096" doc:"where to extract; defaults to the archive's folder"`
	}
}
type folderIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Body struct {
		Path string `json:"path" minLength:"1" maxLength:"4096"`
	}
}
type filePullIn struct {
	UUID string `path:"uuid" pattern:"^[0-9a-f-]{36}$"`
	Body struct {
		URL  string `json:"url" minLength:"8" maxLength:"2048" format:"uri"`
		Path string `json:"path" maxLength:"4096" doc:"a folder ending in / (the file keeps its name) or a full file path"`
	}
}
type transferOut struct{ Body proto.TransferResult }

// pullRequestTimeout covers the node's own limit plus the reply.
const pullRequestTimeout = 31 * time.Minute

func (a *App) registerFiles(api huma.API) {
	// fileOp authorizes, runs op on the node and audits it.
	fileOp := func(ctx context.Context, uuid, action, detail string, op proto.FileOp) (proto.FileOpResult, error) {
		u, s, err := a.authorizeP(ctx, uuid, PermFilesWrite)
		if err != nil {
			return proto.FileOpResult{}, err
		}
		op.UUID = s.UUID
		c, cancel := context.WithTimeout(ctx, 30*time.Minute) // compressing a world takes a while
		defer cancel()
		raw, err := a.Hub.Request(c, s.NodeID, proto.ReqFilesOp, op)
		if err != nil {
			return proto.FileOpResult{}, nodeErr(err)
		}
		var res proto.FileOpResult
		json.Unmarshal(raw, &res)
		a.audit(ctx, u, action, s.UUID, truncate(detail, 500))
		return res, nil
	}

	huma.Register(api, huma.Operation{OperationID: "moveFiles", Method: "POST", Path: "/servers/{uuid}/files/move", Summary: "Rename or move files and folders", DefaultStatus: 204},
		func(ctx context.Context, in *fileMoveIn) (*struct{}, error) {
			var parts []string
			for _, m := range in.Body.Moves {
				parts = append(parts, m.From+" -> "+m.To)
			}
			_, err := fileOp(ctx, in.UUID, "file.move", strings.Join(parts, ", "), proto.FileOp{Op: proto.OpMove, Moves: in.Body.Moves})
			return nil, err
		})

	huma.Register(api, huma.Operation{OperationID: "copyFile", Method: "POST", Path: "/servers/{uuid}/files/copy", Summary: "Duplicate a file or folder", DefaultStatus: 201},
		func(ctx context.Context, in *fileCopyIn) (*pathOut, error) {
			res, err := fileOp(ctx, in.UUID, "file.copy", in.Body.Path, proto.FileOp{Op: proto.OpCopy, Path: in.Body.Path})
			if err != nil {
				return nil, err
			}
			out := &pathOut{}
			out.Body.Path = res.Path
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "chmodFiles", Method: "POST", Path: "/servers/{uuid}/files/chmod", Summary: "Change permissions", DefaultStatus: 204},
		func(ctx context.Context, in *fileChmodIn) (*struct{}, error) {
			var mode uint32
			fmt.Sscanf(in.Body.Mode, "%o", &mode)
			_, err := fileOp(ctx, in.UUID, "file.chmod", in.Body.Mode+" "+strings.Join(in.Body.Paths, ", "), proto.FileOp{Op: proto.OpChmod, Paths: in.Body.Paths, Mode: mode})
			return nil, err
		})

	huma.Register(api, huma.Operation{OperationID: "deleteFiles", Method: "POST", Path: "/servers/{uuid}/files/delete", Summary: "Delete several files and folders", DefaultStatus: 204},
		func(ctx context.Context, in *fileDeleteIn) (*struct{}, error) {
			for _, p := range in.Body.Paths {
				if strings.Trim(p, "/ .") == "" {
					return nil, huma.Error422UnprocessableEntity("the server's main folder cannot be deleted")
				}
			}
			_, err := fileOp(ctx, in.UUID, "file.delete", strings.Join(in.Body.Paths, ", "), proto.FileOp{Op: proto.OpDelete, Paths: in.Body.Paths})
			return nil, err
		})

	huma.Register(api, huma.Operation{OperationID: "compressFiles", Method: "POST", Path: "/servers/{uuid}/files/compress", Summary: "Make an archive of files and folders", DefaultStatus: 201},
		func(ctx context.Context, in *fileCompressIn) (*pathOut, error) {
			res, err := fileOp(ctx, in.UUID, "file.compress", strings.Join(in.Body.Paths, ", "), proto.FileOp{Op: proto.OpCompress, Root: in.Body.Root, Paths: in.Body.Paths, Format: in.Body.Format})
			if err != nil {
				return nil, err
			}
			out := &pathOut{}
			out.Body.Path = res.Path
			return out, nil
		})

	huma.Register(api, huma.Operation{OperationID: "decompressFile", Method: "POST", Path: "/servers/{uuid}/files/decompress", Summary: "Extract an archive", DefaultStatus: 204},
		func(ctx context.Context, in *fileDecompressIn) (*struct{}, error) {
			root := in.Body.Root
			if root == "" {
				root = in.Body.Path[:strings.LastIndex("/"+in.Body.Path, "/")]
			}
			_, err := fileOp(ctx, in.UUID, "file.decompress", in.Body.Path, proto.FileOp{Op: proto.OpDecompress, Path: in.Body.Path, Root: root})
			return nil, err
		})

	huma.Register(api, huma.Operation{OperationID: "createFolder", Method: "POST", Path: "/servers/{uuid}/files/folder", Summary: "Create a folder (and any missing parents)", DefaultStatus: 204},
		func(ctx context.Context, in *folderIn) (*struct{}, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermFilesWrite)
			if err != nil {
				return nil, err
			}
			if _, err := a.Hub.Request(ctx, s.NodeID, proto.ReqFilesMkdir, proto.FileReq{UUID: s.UUID, Path: in.Body.Path}); err != nil {
				return nil, nodeErr(err)
			}
			a.audit(ctx, u, "file.mkdir", s.UUID, in.Body.Path)
			return nil, nil
		})

	huma.Register(api, huma.Operation{OperationID: "pullFile", Method: "POST", Path: "/servers/{uuid}/files/pull",
		Summary:     "Download a file from the internet into the server",
		Description: "The node fetches the URL itself. Private and local addresses are refused unless the node allows them.", DefaultStatus: 201},
		func(ctx context.Context, in *filePullIn) (*transferOut, error) {
			u, s, err := a.authorizeP(ctx, in.UUID, PermFilesWrite)
			if err != nil {
				return nil, err
			}
			c, cancel := context.WithTimeout(ctx, pullRequestTimeout)
			defer cancel()
			raw, err := a.Hub.Request(c, s.NodeID, proto.ReqFilesPull, proto.Transfer{UUID: s.UUID, URL: in.Body.URL, Path: in.Body.Path})
			if err != nil {
				return nil, nodeErr(err)
			}
			out := &transferOut{}
			json.Unmarshal(raw, &out.Body)
			a.audit(ctx, u, "file.pull", s.UUID, truncate(in.Body.URL, 300)+" -> "+out.Body.Path)
			return out, nil
		})
}
