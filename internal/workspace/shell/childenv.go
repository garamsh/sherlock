package shell

import "os"

// childEnv returns the minimal environment for a child process the
// executor spawns: PATH, HOME, and LANG only. The platform's own
// variables (provider API keys and the like) are never inherited
// implicitly (ADR 0003); the request's Env entries apply on top.
func childEnv() []string {
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
