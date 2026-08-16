package main

import (
	"os"
	"testing"
)

func TestZZDump(t *testing.T) {
	r := committedRun(t)
	for _, s := range RenderTables(r) {
		if err := os.WriteFile("/tmp/section_"+s.Name+".md", []byte(s.Tables+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
