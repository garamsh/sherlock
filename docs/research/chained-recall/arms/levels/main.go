// Command levels is arm H of the #874 chained-recall study: batch-4 mids
// plus a second level of group episodes, one per contiguous window of G
// mids, ranked with the mids in one collapsed list. See README.md.
package main

import (
	"fmt"

	"github.com/garamsh/sherlock/docs/research/chained-recall/study"
)

func main() { study.Main(New()) }

// New returns arm H.
func New() study.Arm { return arm{} }

type arm struct{}

func (arm) Name() string         { return "H" }
func (arm) Settings() []string   { return []string{"g4", "g8"} }
func (arm) Instructions() string { return "" }

// groupSize reads G from a setting "g<G>".
func groupSize(setting string) (int, error) {
	var g int
	if _, err := fmt.Sscanf(setting, "g%d", &g); err != nil || g < 2 || fmt.Sprintf("g%d", g) != setting {
		return 0, fmt.Errorf("setting %q is not g<G> with G of at least 2", setting)
	}
	return g, nil
}
