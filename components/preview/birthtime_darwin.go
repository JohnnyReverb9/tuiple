package preview

import (
	"os"
	"syscall"
	"time"
)

// birthTime is when the file was created; macOS records it, unlike most
// Unix systems.
func birthTime(info os.FileInfo) (time.Time, bool) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}, false
	}
	return time.Unix(st.Birthtimespec.Sec, st.Birthtimespec.Nsec), true
}
