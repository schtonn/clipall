//go:build windows

package main

import (
	"strings"
	"testing"
)

func TestWindowsRunnerScriptPreservesArgumentsAndExitCode(t *testing.T) {
	script := windowsRunnerScript(
		`C:\Program Files\clipall\clipall.exe`,
		[]string{"--peers", "one:9876,two:9876", "--config", `C:\Users\O'Brien\clipall.yaml`},
		`C:\Users\O'Brien\AppData\Local\clipall\clipall.log`,
	)
	for _, want := range []string{
		`& 'C:\Program Files\clipall\clipall.exe'`,
		`'one:9876,two:9876'`,
		`'C:\Users\O''Brien\clipall.yaml'`,
		`*>> $log`,
		`exit $LASTEXITCODE`,
		`-ge 5242880`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("runner script missing %q:\n%s", want, script)
		}
	}
}

func TestWindowsTaskHasRestartAndSingleInstanceSettings(t *testing.T) {
	script := windowsTaskInstallScript(`C:\clipall.exe`, []string{"--files"}, `C:\clipall.log`, 42)
	for _, want := range []string{
		"New-ScheduledTaskTrigger -AtLogOn",
		"-RestartCount 999",
		"-RestartInterval (New-TimeSpan -Minutes 1)",
		"-MultipleInstances IgnoreNew",
		"-ExecutionTimeLimit ([TimeSpan]::Zero)",
		"Register-ScheduledTask",
		"$_.ProcessId -ne 42",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("task script missing %q:\n%s", want, script)
		}
	}
}
