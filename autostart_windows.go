//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const (
	windowsTaskName = "clipall"
	windowsRunKey   = `Software\Microsoft\Windows\CurrentVersion\Run`
	windowsRunName  = "clipall"
)

func windowsArgumentLine(args []string) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		parts = append(parts, syscall.EscapeArg(arg))
	}
	return strings.Join(parts, " ")
}

func powershellLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func windowsLogPath() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		var err error
		base, err = os.UserCacheDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(base, "clipall", "clipall.log"), nil
}

// windowsRunnerScript keeps a small rotating log and preserves clipall's exit
// code so Task Scheduler can restart it after a failure.
func windowsRunnerScript(executable string, args []string, logPath string) string {
	invocation := "& " + powershellLiteral(executable)
	for _, arg := range args {
		invocation += " " + powershellLiteral(arg)
	}
	return strings.Join([]string{
		"$log = " + powershellLiteral(logPath),
		"if ((Test-Path -LiteralPath $log) -and ((Get-Item -LiteralPath $log).Length -ge 5242880)) { Move-Item -Force -LiteralPath $log -Destination ($log + '.1') }",
		invocation + " *>> $log",
		"exit $LASTEXITCODE",
	}, "; ")
}

func windowsTaskInstallScript(executable string, args []string, logPath string, installerPID int) string {
	runnerArgs := windowsArgumentLine([]string{
		"-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command",
		windowsRunnerScript(executable, args, logPath),
	})
	return strings.Join([]string{
		"$ErrorActionPreference = 'Stop'",
		"$taskName = " + powershellLiteral(windowsTaskName),
		"$existing = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue",
		"if ($null -ne $existing) { Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue }",
		"Get-CimInstance Win32_Process -ErrorAction SilentlyContinue | Where-Object { $_.ExecutablePath -eq " + powershellLiteral(executable) + " -and $_.ProcessId -ne " + strconv.Itoa(installerPID) + " } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }",
		"$identity = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name",
		"$action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument " + powershellLiteral(runnerArgs),
		"$trigger = New-ScheduledTaskTrigger -AtLogOn -User $identity",
		"$principal = New-ScheduledTaskPrincipal -UserId $identity -LogonType Interactive -RunLevel Limited",
		"$settings = New-ScheduledTaskSettingsSet -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit ([TimeSpan]::Zero) -MultipleInstances IgnoreNew -StartWhenAvailable -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -Hidden",
		"Register-ScheduledTask -TaskName $taskName -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Description 'clipall clipboard synchronization' -Force | Out-Null",
	}, "; ")
}

func runHiddenPowerShell(script string) error {
	command := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("PowerShell: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func removeLegacyRunEntry() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, windowsRunKey, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open legacy Windows Run key: %w", err)
	}
	defer key.Close()
	if err := key.DeleteValue(windowsRunName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("delete legacy Windows Run value: %w", err)
	}
	return nil
}

func installAutostart(executable string, args []string) error {
	logPath, err := windowsLogPath()
	if err != nil {
		return fmt.Errorf("find log directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0700); err != nil {
		return fmt.Errorf("create log directory: %w", err)
	}
	if err := runHiddenPowerShell(windowsTaskInstallScript(executable, args, logPath, os.Getpid())); err != nil {
		return fmt.Errorf("register Windows scheduled task: %w", err)
	}
	if err := removeLegacyRunEntry(); err != nil {
		return err
	}
	return nil
}

func startAutostartNow(string, []string) error {
	script := "$ErrorActionPreference = 'Stop'; Start-ScheduledTask -TaskName " + powershellLiteral(windowsTaskName)
	if err := runHiddenPowerShell(script); err != nil {
		return fmt.Errorf("start Windows scheduled task: %w", err)
	}
	return nil
}

func uninstallAutostart() error {
	executable, _ := os.Executable()
	script := strings.Join([]string{
		"$taskName = " + powershellLiteral(windowsTaskName),
		"$existing = Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue",
		"if ($null -ne $existing) { Stop-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue; Unregister-ScheduledTask -TaskName $taskName -Confirm:$false }",
		"Get-CimInstance Win32_Process -ErrorAction SilentlyContinue | Where-Object { $_.ExecutablePath -eq " + powershellLiteral(executable) + " -and $_.ProcessId -ne " + strconv.Itoa(os.Getpid()) + " } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }",
	}, "; ")
	if err := runHiddenPowerShell(script); err != nil {
		return fmt.Errorf("remove Windows scheduled task: %w", err)
	}
	return removeLegacyRunEntry()
}
