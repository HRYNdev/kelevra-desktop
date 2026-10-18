//go:build !windows

package hranenie

import "os"

// zakrytFayl — вне Windows достаточно прав 0600 (читает только владелец).
func zakrytFayl(put string) error { return os.Chmod(put, 0o600) }
