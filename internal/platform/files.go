package platform

import (
	"cmp"
	"errors"
	"io"
	"io/fs"
	"os"
	"strconv"

	"schemo/internal/domain"
)

// FileReader reads a file whole, up to a limit. Zero means domain.MaxBodyBytes.
type FileReader struct {
	MaxBytes int64
}

func NewFileReader() *FileReader {
	return &FileReader{}
}

// Read refuses a file over the limit rather than truncating it; one extra byte tells "exactly at the
// limit" from "cut".
func (r *FileReader) Read(path string) ([]byte, error) {
	limit := cmp.Or(r.MaxBytes, domain.MaxBodyBytes)

	file, err := os.Open(path)
	if err != nil {
		return nil, ReadError(path, err)
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, ReadError(path, err)
	}
	if int64(len(data)) > limit {
		return nil, TooLarge(path, limit)
	}
	return data, nil
}

// TooLarge refuses a file or document over limit. The limit is given in MiB, the unit the message
// names.
func TooLarge(path string, limit int64) error {
	return domain.Refuse(domain.CodeFileTooLarge, domain.ErrNotAllowed, domain.Args{
		"path":  path,
		"limit": strconv.Itoa(int(limit >> 20)),
	})
}

// ReadError describes a failure reading a local file or directory.
func ReadError(path string, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return domain.Refuse(domain.CodeFileMissing, domain.ErrNotFound, domain.Args{"path": path})
	}
	return domain.Refuse(domain.CodeFileUnreadable, domain.ErrNotAllowed,
		domain.Args{"path": path, "reason": err.Error()})
}
