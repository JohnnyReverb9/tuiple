//go:build !unix

package filesystem

// FreeSpace is not available on this platform.
func FreeSpace(string) (int64, bool) { return 0, false }
