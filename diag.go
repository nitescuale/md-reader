//go:build windows

package main

// Startup diagnostics: every startup step is logged to a file next to the
// executable (fallback %TEMP%), and any Go panic is caught, logged and shown in
// a message box instead of vanishing silently (a GUI-subsystem process has no
// console, so a panic would otherwise leave no trace at all).

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"syscall"
	"time"
	"unsafe"
)

var ntdll = syscall.NewLazyDLL("ntdll.dll")

var (
	logPath   string
	lastStep  = "initialisation"
	consoleOn bool
)

func initDiag() {
	exe := exePath()
	dir := filepath.Dir(exe)
	cand := filepath.Join(dir, "mdreader-log.txt")
	if f, err := os.OpenFile(cand, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		f.Close()
		logPath = cand
	} else {
		tmp := os.Getenv("TEMP")
		if tmp == "" {
			tmp = os.TempDir()
		}
		logPath = filepath.Join(tmp, "mdreader-log.txt")
	}
	os.WriteFile(logPath, nil, 0o644)

	consoleOn = false
	if h, _, _ := kernel32.NewProc("GetConsoleWindow").Call(); h != 0 {
		consoleOn = true
	}

	logf("=== MdReader %s ===", appVersion)
	logf("exe        : %s", exe)
	logf("log        : %s", logPath)
	logf("go         : %s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	logf("windows    : %s", windowsVersion())
	logf("args       : %q", os.Args)
	logf("console    : %v", consoleOn)
}

func logf(format string, a ...any) {
	line := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, a...))
	if logPath != "" {
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			fmt.Fprintln(f, line)
			f.Close()
		}
	}
	if consoleOn {
		fmt.Println(line)
	}
}

// step records the startup phase currently running, so a hard crash leaves the
// last reached phase in the log.
func step(name string) {
	lastStep = name
	logf("STEP %s", name)
}

type osVersionInfoExW struct {
	Size             uint32
	MajorVersion     uint32
	MinorVersion     uint32
	BuildNumber      uint32
	PlatformID       uint32
	CSDVersion       [128]uint16
	ServicePackMajor uint16
	ServicePackMinor uint16
	SuiteMask        uint16
	ProductType      byte
	Reserved         byte
}

func windowsVersion() string {
	p := ntdll.NewProc("RtlGetVersion")
	if p.Find() != nil {
		return "inconnue"
	}
	var vi osVersionInfoExW
	vi.Size = uint32(unsafe.Sizeof(vi))
	if r, _, _ := p.Call(uintptr(unsafe.Pointer(&vi))); r != 0 {
		return fmt.Sprintf("RtlGetVersion a échoué (%d)", r)
	}
	return fmt.Sprintf("%d.%d build %d (product type %d)", vi.MajorVersion, vi.MinorVersion, vi.BuildNumber, vi.ProductType)
}

// handlePanic reports a recovered panic in a visible way.
func handlePanic(where string, r any) {
	logf("PANIC (%s): %v\n%s", where, r, debug.Stack())
	msg := fmt.Sprintf("MdReader a rencontré une erreur.\n\nÉtape : %s\nErreur : %v\n\nUn journal a été écrit ici :\n%s",
		lastStep, r, logPath)
	pMessageBoxW.Call(0, uintptr(unsafe.Pointer(utf16Ptr(msg))),
		uintptr(unsafe.Pointer(utf16Ptr("MdReader — erreur"))), MB_OK|MB_ICONERROR)
	waitIfConsole()
}

// fatalDialog is used when the application cannot continue at all.
func fatalDialog(what string, detail string) {
	logf("FATAL (%s): %s", what, detail)
	msg := fmt.Sprintf("MdReader ne peut pas démarrer.\n\nÉtape : %s\n%s\n\nJournal :\n%s",
		lastStep, detail, logPath)
	pMessageBoxW.Call(0, uintptr(unsafe.Pointer(utf16Ptr(msg))),
		uintptr(unsafe.Pointer(utf16Ptr("MdReader — démarrage impossible"))), MB_OK|MB_ICONERROR)
	waitIfConsole()
}

// waitIfConsole keeps a console window open so the trace can be read.
func waitIfConsole() {
	if !consoleOn {
		return
	}
	fmt.Println("\n--- appuyez sur Entrée pour fermer ---")
	buf := make([]byte, 1)
	os.Stdin.Read(buf)
}
