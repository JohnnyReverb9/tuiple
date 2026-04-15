package filesystem

import (
	"io"
	"os"
	"path/filepath"
)

// CopyDir recursively copies a directory tree, creating destination if needed.
func CopyDir(src string, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := CopyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := CopyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// CopyFile copies a single file from src to dst.
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err = io.Copy(out, in); err != nil {
		return err
	}

	// Copy permissions
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chmod(dst, info.Mode())
}

// Move moves or renames a file or directory.
func Move(src, dst string) error {
	return os.Rename(src, dst)
}

// Remove deletes a file or directory path.
func Remove(path string, isDir bool) error {
	if isDir {
		return os.RemoveAll(path)
	}
	return os.Remove(path)
}

// CreateFile creates an empty file.
func CreateFile(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL, 0666)
	if err != nil {
		return err
	}
	return file.Close()
}

// CreateDir creates a new directory.
func CreateDir(path string) error {
	return os.Mkdir(path, 0755)
}
