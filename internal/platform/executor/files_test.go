package executor

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalWritesDoNotFollowSymlinksOrChangeTheirTargets(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "operator")
	link := filepath.Join(dir, "managed")
	if err := os.WriteFile(target, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	ex := NewLocal()
	if err := ex.WriteFile(context.Background(), link, []byte("overwrite"), 0o644); err == nil {
		t.Fatal("symlink accepted")
	}
	content, _ := os.ReadFile(target)
	info, _ := os.Stat(target)
	if string(content) != "secret" || info.Mode().Perm() != 0o600 {
		t.Fatal("operator file was modified")
	}
	if err := ex.WriteFileExclusive(context.Background(), link, nil, 0o644); err == nil {
		t.Fatal("exclusive write accepted existing symlink")
	}
}
func TestExclusiveWritePreservesFirstFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	ex := NewLocal()
	if err := WriteFileExclusive(context.Background(), ex, path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileExclusive(context.Background(), ex, path, []byte("second"), 0o644); !errors.Is(err, os.ErrExist) {
		t.Fatalf("err=%v", err)
	}
	content, _ := os.ReadFile(path)
	if string(content) != "first" {
		t.Fatal("first file overwritten")
	}
}
func TestLocalReadRejectsOversizedAndNonregularFiles(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "big")
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Truncate(MaxFileBytes + 1)
	_ = f.Close()
	ex := NewLocal()
	if _, err := ex.ReadFile(context.Background(), file); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("err=%v", err)
	}
	if _, err := ex.ReadFile(context.Background(), dir); err == nil {
		t.Fatal("directory accepted")
	}
	link := filepath.Join(dir, "link")
	_ = os.Symlink(file, link)
	if _, err := ex.ReadFile(context.Background(), link); err == nil {
		t.Fatal("symlink accepted")
	}
}
func TestFileReaderBoundsGrowthAndHonorsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := newFileReader(ctx, io.NopCloser(strings.NewReader("12345")), 4)
	defer r.Close()
	data, err := io.ReadAll(r)
	if string(data) != "1234" || !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("data=%q err=%v", data, err)
	}
	cancel()
	if _, err := r.Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}
func TestRunInputKeepsContentOutOfArgumentsAndPreservesBytes(t *testing.T) {
	input := "secret\n\n"
	output, err := RunInput(context.Background(), NewLocal(), "cat", []byte(input))
	if err != nil || output != input {
		t.Fatalf("output=%q err=%v", output, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := NewLocal().WriteFile(ctx, filepath.Join(t.TempDir(), "file"), nil, 0o600); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}
