// Command shipped is arm S of the #874 study: the shipped structure at
// 03cbdff, at batch 4 and 8, with the model free to chain memory_search.
package main

import "github.com/garamsh/sherlock/research/chained-recall/study"

func main() {
	study.Main(study.ShippedArm{ArmName: "S", Batches: []string{"b4", "b8"}})
}
