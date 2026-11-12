package application

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"path"
	"strings"

	"pier/internal/platform/apperr"
)

const (
	// MaxArchiveBytes bounds the compressed upload of `pierctl up`.
	MaxArchiveBytes = 200 << 20
	// maxArchiveExpandedBytes bounds the extracted size (gzip bombs).
	maxArchiveExpandedBytes = 1 << 30
	maxArchiveEntries       = 100_000
	maxSymlinkHops          = 40
)

// archiveSummary describes an accepted archive.
type archiveSummary struct {
	Files int
	Bytes int64
}

// archiveSanitizer rewrites an uploaded tar.gz into one that is safe to
// extract anywhere: only regular files, directories and links that stay
// inside the root, owned by root, without special permission bits.
type archiveSanitizer struct {
	out     *tar.Writer
	summary archiveSummary
	entries int
	names   map[string]bool
	dirs    map[string]bool
	files   map[string]bool
	links   map[string]string
}

// sanitizeArchive reads a gzip-compressed tar from in and writes the cleaned
// gzip-compressed tar to out. Any unsafe entry rejects the whole archive.
func sanitizeArchive(in io.Reader, out io.Writer) (archiveSummary, error) {
	limited := &limitedReader{reader: in, remaining: MaxArchiveBytes}
	compressed, err := gzip.NewReader(limited)
	if err != nil {
		return archiveSummary{}, archiveReadError(limited, err)
	}
	zipped := gzip.NewWriter(out)
	s := &archiveSanitizer{
		out: tar.NewWriter(zipped), names: map[string]bool{}, dirs: map[string]bool{},
		files: map[string]bool{}, links: map[string]string{},
	}
	reader := tar.NewReader(compressed)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return archiveSummary{}, archiveReadError(limited, err)
		}
		if err := s.add(header, reader); err != nil {
			return archiveSummary{}, archiveReadError(limited, err)
		}
	}
	if err := s.checkLinks(); err != nil {
		return archiveSummary{}, err
	}
	if s.summary.Files == 0 {
		return archiveSummary{}, apperr.Invalid("O arquivo enviado não contém nenhum arquivo.")
	}
	if err := s.out.Close(); err != nil {
		return archiveSummary{}, err
	}
	return s.summary, zipped.Close()
}

func archiveReadError(limited *limitedReader, err error) error {
	if limited.exceeded {
		return apperr.Invalid("O arquivo enviado excede o limite de 200 MiB.")
	}
	if _, isAppErr := apperr.As(err); isAppErr {
		return err
	}
	return apperr.Invalid("O arquivo enviado não é um .tar.gz válido.")
}

func (s *archiveSanitizer) add(header *tar.Header, content io.Reader) error {
	if header.Typeflag == tar.TypeXGlobalHeader {
		return nil
	}
	name, err := archiveEntryName(header.Name)
	if err != nil || name == "." {
		return err
	}
	if s.entries++; s.entries > maxArchiveEntries {
		return apperr.Invalid("O arquivo enviado tem mais de %d entradas.", maxArchiveEntries)
	}
	if s.names[name] {
		return apperr.Invalid("O arquivo contém a entrada %s mais de uma vez.", name)
	}
	if err := s.checkParents(name); err != nil {
		return err
	}
	s.names[name] = true
	clean := &tar.Header{Name: name, ModTime: header.ModTime, Format: tar.FormatPAX}
	switch header.Typeflag {
	case tar.TypeDir:
		s.dirs[name] = true
		clean.Typeflag, clean.Mode, clean.Name = tar.TypeDir, 0o755, name+"/"
	case tar.TypeReg, tar.TypeRegA:
		return s.addFile(clean, header.Size, header.Mode, content)
	case tar.TypeSymlink:
		if err := s.addSymlink(name, header.Linkname); err != nil {
			return err
		}
		clean.Typeflag, clean.Mode, clean.Linkname = tar.TypeSymlink, 0o777, header.Linkname
	case tar.TypeLink:
		target, err := archiveEntryName(header.Linkname)
		if err != nil || !s.files[target] {
			return apperr.Invalid("O link %s aponta para fora do arquivo ou para algo que não é um arquivo comum.", name)
		}
		clean.Typeflag, clean.Mode, clean.Linkname = tar.TypeLink, 0o644, target
	default:
		return apperr.Invalid("O arquivo contém uma entrada não suportada (dispositivo, FIFO ou similar): %s.", name)
	}
	return s.out.WriteHeader(clean)
}

func (s *archiveSanitizer) addFile(clean *tar.Header, size, mode int64, content io.Reader) error {
	if size < 0 {
		return apperr.Invalid("O arquivo enviado não é um .tar.gz válido.")
	}
	if s.summary.Bytes += size; s.summary.Bytes > maxArchiveExpandedBytes {
		return apperr.Invalid("O conteúdo do arquivo passa de 1 GiB depois de descompactado.")
	}
	clean.Typeflag, clean.Size, clean.Mode = tar.TypeReg, size, 0o644
	if mode&0o111 != 0 {
		clean.Mode = 0o755
	}
	if err := s.out.WriteHeader(clean); err != nil {
		return err
	}
	if _, err := io.CopyN(s.out, content, size); err != nil {
		return err
	}
	s.files[clean.Name] = true
	s.summary.Files++
	return nil
}

func (s *archiveSanitizer) addSymlink(name, target string) error {
	if target == "" || path.IsAbs(target) || strings.ContainsAny(target, "\x00\\") {
		return apperr.Invalid("O link simbólico %s aponta para fora do arquivo.", name)
	}
	if s.dirs[name] {
		return apperr.Invalid("O arquivo contém um caminho inseguro: %s.", name)
	}
	s.links[name] = target
	return nil
}

// checkParents refuses entries written through a symbolic link (extraction
// would follow it) and links replacing a directory that already has entries.
func (s *archiveSanitizer) checkParents(name string) error {
	for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
		if _, isLink := s.links[parent]; isLink || s.files[parent] {
			return apperr.Invalid("O arquivo contém um caminho inseguro: %s.", name)
		}
		s.dirs[parent] = true
	}
	return nil
}

// checkLinks resolves every symbolic link against all the others, as the
// kernel would after extraction, and refuses any that leaves the root.
func (s *archiveSanitizer) checkLinks() error {
	for name := range s.links {
		if !s.resolvesInside(name) {
			return apperr.Invalid("O link simbólico %s aponta para fora do arquivo.", name)
		}
	}
	return nil
}

func (s *archiveSanitizer) resolvesInside(name string) bool {
	pending := strings.Split(name, "/")
	resolved := []string{}
	for hops := 0; len(pending) > 0; {
		part := pending[0]
		pending = pending[1:]
		switch part {
		case "", ".":
			continue
		case "..":
			if len(resolved) == 0 {
				return false
			}
			resolved = resolved[:len(resolved)-1]
			continue
		}
		resolved = append(resolved, part)
		target, isLink := s.links[strings.Join(resolved, "/")]
		if !isLink {
			continue
		}
		if hops++; hops > maxSymlinkHops {
			return false
		}
		resolved = resolved[:len(resolved)-1]
		pending = append(strings.Split(target, "/"), pending...)
	}
	return true
}

// archiveEntryName validates a member name and returns it cleaned and relative.
func archiveEntryName(raw string) (string, error) {
	name := strings.TrimPrefix(raw, "./")
	if name == "" || name == "." {
		return ".", nil
	}
	if strings.ContainsAny(name, "\x00\\") || path.IsAbs(name) || hasParentSegment(strings.TrimSuffix(name, "/")) {
		return "", apperr.Invalid("O arquivo contém um caminho inseguro: %s.", raw)
	}
	return path.Clean(name), nil
}

// limitedReader fails once more than remaining bytes are read.
type limitedReader struct {
	reader    io.Reader
	remaining int64
	exceeded  bool
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.remaining <= 0 {
		var probe [1]byte
		if n, _ := l.reader.Read(probe[:]); n > 0 {
			l.exceeded = true
			return 0, errors.New("archive too large")
		}
		return 0, io.EOF
	}
	if int64(len(p)) > l.remaining {
		p = p[:l.remaining]
	}
	n, err := l.reader.Read(p)
	l.remaining -= int64(n)
	return n, err
}
