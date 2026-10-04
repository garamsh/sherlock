//go:build !unix

package sqlite

import (
	"errors"
	"os"
)

// lockWriter reports that the writer lock needs flock, which this
// platform lacks. Production deployment is Linux and the non-Unix build
// exists for tooling; failing keeps a store from opening unlocked.
func lockWriter(string) (*os.File, error) {
	return nil, errors.New("memory writer lock: flock is unavailable on this platform")
}
