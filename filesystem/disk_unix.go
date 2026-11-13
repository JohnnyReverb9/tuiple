//go:build unix

package filesystem

import "syscall"

// FreeSpace reports how many bytes are still available to the user on
// the volume holding path — the figure Finder's status bar shows.
func FreeSpace(path string) (int64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, false
	}
	return int64(st.Bavail) * int64(st.Bsize), true
}
