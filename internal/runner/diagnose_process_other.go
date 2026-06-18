//go:build !unix

package runner

import "os/exec"

func isolateDiagnoseChildProcessGroup(cmd *exec.Cmd) {}
