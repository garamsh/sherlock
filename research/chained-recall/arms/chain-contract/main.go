// Command chain-contract is arm M of the #874 study: the shipped structure
// at batch 4, with an instruction to follow what a result names
// (research/associative-memory/ §Candidates for sherlock, M). It
// shares S's store, so its offline records equal S's; it differs live.
package main

import "github.com/garamsh/sherlock/research/chained-recall/study"

// Instruction is M's one change. Production would carry it in the
// platform contract's §Source-linked memory; the study appends it to the
// ego, the only instruction text a session takes without a code change.
const Instruction = "When a recalled memory or a memory_search result names a person, place, organization, project or rule that your task depends on, call memory_search for that name next, and keep following each name the results lead to until you have what the task asks for."

func main() {
	study.Main(study.ShippedArm{ArmName: "M", Instruction: Instruction, Batches: []string{"b4"}})
}
