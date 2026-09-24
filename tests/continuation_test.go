package tests

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func continuationFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	require.NoError(t, filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported snapshot entry %s", path)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[relative] = string(body)
		return nil
	}))
	return files
}

func continuationRestore(t *testing.T, root string, files map[string]string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(root, 0700))
	for path, body := range files {
		destination := filepath.Join(root, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(destination), 0700))
		require.NoError(t, os.WriteFile(destination, []byte(body), 0600))
	}
}

func continuationStop(t *testing.T, cmd *exec.Cmd, exited <-chan error, signal os.Signal, dir string) {
	t.Helper()
	require.NoError(t, cmd.Process.Signal(signal))
	select {
	case err := <-exited:
		if signal == syscall.SIGTERM {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("continuation process did not exit")
	}
	if log, ok := cmd.Stdout.(*os.File); ok {
		body, err := os.ReadFile(log.Name())
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("process-%d.log", cmd.Process.Pid)), body, 0600))
	}
}
