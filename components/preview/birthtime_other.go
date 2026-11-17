//go:build !darwin

package preview

import (
	"os"
	"time"
)

// birthTime is not available off macOS.
func birthTime(os.FileInfo) (time.Time, bool) { return time.Time{}, false }
