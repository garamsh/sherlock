package tool

import "os"

// ChildEnv returns the minimal environment for a child process spawned by
// a code-executing tool: PATH, HOME, and LANG only. The platform's own
// variables (provider API keys and the like) are never inherited
// implicitly (ADR 0003); operator-configured entries such as a script
// tool's exec.env are applied on top by the caller.
func ChildEnv() []string {
	path := os.Getenv("PATH")
	if path == "" {
		path = "/usr/local/bin:/usr/bin:/bin"
	}
	lang := os.Getenv("LANG")
	if lang == "" {
		lang = "C.UTF-8"
	}
	return []string{
		"PATH=" + path,
		"HOME=" + os.TempDir(),
		"LANG=" + lang,
	}
}
