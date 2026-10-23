package platform

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"strconv"

	"schemo/internal/domain"
)

// FileReader reads a file whole, up to a limit. Zero means the default.
type FileReader struct {
	MaxBytes int64
}

// DefaultMaxBytes is what a body is capped at when nothing says otherwise. It is the same ceiling
// the engine reads a response under, because a request and its answer are the same size of problem.
const DefaultMaxBytes int64 = 8 << 20

func NewFileReader() *FileReader {
	return &FileReader{MaxBytes: DefaultMaxBytes}
}

// Read treats a file larger than the limit as an error rather than a truncation: a truncated upload
// is a corrupt file at the other end, worse than a send that did not happen. The extra byte read
// past the limit tells "ends exactly at the limit" from "was cut there".
func (r *FileReader) Read(path string) ([]byte, error) {
	limit := r.MaxBytes
	if limit <= 0 {
		limit = DefaultMaxBytes
	}

	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, domain.Refuse(domain.CodeFileMissing, domain.ErrNotFound, domain.Args{"path": path})
		}
		return nil, domain.Refuse(domain.CodeFileUnreadable, domain.ErrNotAllowed,
			domain.Args{"path": path, "reason": err.Error()})
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, domain.Refuse(domain.CodeFileUnreadable, domain.ErrNotAllowed,
			domain.Args{"path": path, "reason": err.Error()})
	}
	if int64(len(data)) > limit {
		// The limit is named in the unit its sentence names, so the catalogue never has to say "bytes".
		return nil, domain.Refuse(domain.CodeFileTooLarge, domain.ErrNotAllowed, domain.Args{
			"path":  path,
			"limit": strconv.Itoa(int(limit >> 20)),
		})
	}
	return data, nil
}
