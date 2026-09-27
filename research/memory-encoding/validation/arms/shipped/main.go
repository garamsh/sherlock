// Command shipped is arm S of the #890 validation study: the shipped
// structure over the shared batch-4 store, read as production reads it
// (study.ShippedArm).
package main

import "github.com/garamsh/sherlock/research/chained-recall/study"

func main() {
	study.Main(study.ShippedArm{ArmName: "S", Batches: []string{"b4"}})
}
