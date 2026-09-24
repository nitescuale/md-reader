//go:build windows

package main

// Persistent settings and the .md file association (per-user, no admin rights
// required: everything lives under HKCU\Software\Classes).

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

type Config struct {
	Theme     int      `json:"theme"`
	Scale     float64  `json:"scale"`
	Recent    []string `json:"recent"`
	X         int      `json:"x"`
	Y         int      `json:"y"`
	W         int      `json:"w"`
	H         int      `json:"h"`
	Maximized bool     `json:"maximized"`

	// RenderMode remembers which way of loading the rich text worked on this
	// machine (see the rm* constants in main.go). 0 = not known yet.
	RenderMode int `json:"renderMode,omitempty"`

	lastMod int64 `json:"-"`
}

// ------------------------------------------------------------------ config

func exePath() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err2 := filepath.EvalSymlinks(p); err2 == nil {
		p = r
	}
	return p
}

// configPath returns the settings file: portable (next to the executable) when
// that folder is writable, otherwise %APPDATA%\MdReader.
func configPath() string {
	if exe := exePath(); exe != "" {
		p := filepath.Join(filepath.Dir(exe), "mdreader.json")
		if f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			f.Close()
			return p
		}
	}
	dir := os.Getenv("APPDATA")
	if dir == "" {
		dir = os.TempDir()
	}
	d := filepath.Join(dir, "MdReader")
	os.MkdirAll(d, 0o755)
	return filepath.Join(d, "config.json")
}

func loadConfig(path *string) Config {
	c := Config{Theme: 0, Scale: 1}
	*path = configPath()
	if data, err := os.ReadFile(*path); err == nil {
		json.Unmarshal(data, &c)
	}
	if c.Scale <= 0.3 || c.Scale > 4 {
		c.Scale = 1
	}
	return c
}

func saveConfig(path string, c Config) {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(path, data, 0o644)
}

// ------------------------------------------------------------- dark mode

const personalizeKey = `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`

func systemDark() bool {
	return regReadDword(personalizeKey, "AppsUseLightTheme", 1) == 0
}

// ------------------------------------------------------------------ registry

func regReadDword(sub, name string, def uint32) uint32 {
	var h uintptr
	sp, err := syscall.UTF16PtrFromString(sub)
	if err != nil {
		return def
	}
	r, _, _ := advapi32.NewProc("RegOpenKeyExW").Call(HKEY_CURRENT_USER,
		uintptr(unsafe.Pointer(sp)), 0, KEY_READ, uintptr(unsafe.Pointer(&h)))
	if r != 0 {
		return def
	}
	defer pRegCloseKey.Call(h)
	np, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return def
	}
	var val uint32
	var size uint32 = 4
	var typ uint32
	r, _, _ = pRegQueryValueExW.Call(h, uintptr(unsafe.Pointer(np)), 0,
		uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&val)), uintptr(unsafe.Pointer(&size)))
	if r != 0 {
		return def
	}
	return val
}

func regCreateKey(sub string) (uintptr, error) {
	var h, disp uintptr
	sp, err := syscall.UTF16PtrFromString(sub)
	if err != nil {
		return 0, err
	}
	r, _, e := pRegCreateKeyExW.Call(HKEY_CURRENT_USER, uintptr(unsafe.Pointer(sp)), 0, 0, 0,
		KEY_ALL_ACCESS, 0, uintptr(unsafe.Pointer(&h)), uintptr(unsafe.Pointer(&disp)))
	if r != 0 {
		return 0, errors.New("RegCreateKeyEx " + sub + ": " + e.Error())
	}
	return h, nil
}

func regSetString(h uintptr, name, value string) error {
	np, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	vp, err := syscall.UTF16PtrFromString(value)
	if err != nil {
		return err
	}
	n := uintptr((len(value) + 1) * 2)
	r, _, e := pRegSetValueExW.Call(h, uintptr(unsafe.Pointer(np)), 0, REG_SZ,
		uintptr(unsafe.Pointer(vp)), n)
	if r != 0 {
		return errors.New("RegSetValueEx: " + e.Error())
	}
	return nil
}

func regSetKeyValue(sub, name, value string) error {
	h, err := regCreateKey(sub)
	if err != nil {
		return err
	}
	defer pRegCloseKey.Call(h)
	return regSetString(h, name, value)
}

func regDeleteKey(sub string) {
	sp, err := syscall.UTF16PtrFromString(sub)
	if err != nil {
		return
	}
	if pRegDeleteTreeW.Find() == nil {
		pRegDeleteTreeW.Call(HKEY_CURRENT_USER, uintptr(unsafe.Pointer(sp)))
	}
}

func regGetString(sub, name string) string {
	var h uintptr
	sp, err := syscall.UTF16PtrFromString(sub)
	if err != nil {
		return ""
	}
	r, _, _ := advapi32.NewProc("RegOpenKeyExW").Call(HKEY_CURRENT_USER,
		uintptr(unsafe.Pointer(sp)), 0, KEY_READ, uintptr(unsafe.Pointer(&h)))
	if r != 0 {
		return ""
	}
	defer pRegCloseKey.Call(h)
	np, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return ""
	}
	buf := make([]uint16, 1024)
	size := uint32(len(buf) * 2)
	var typ uint32
	r, _, _ = pRegQueryValueExW.Call(h, uintptr(unsafe.Pointer(np)), 0,
		uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r != 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

// ------------------------------------------------------------------ assoc

const progID = "MdReader.md"

var markdownExts = []string{".md", ".markdown", ".mdown", ".mkd"}

// installAssoc registers MdReader as the per-user handler for markdown files.
func installAssoc() error {
	exe := exePath()
	if exe == "" {
		return errors.New("chemin de l'exécutable introuvable")
	}
	cmd := `"` + exe + `" "%1"`

	if err := regSetKeyValue(`Software\Classes\`+progID, "", "Document Markdown"); err != nil {
		return err
	}
	regSetKeyValue(`Software\Classes\`+progID+`\DefaultIcon`, "", exe+",0")
	if err := regSetKeyValue(`Software\Classes\`+progID+`\shell\open\command`, "", cmd); err != nil {
		return err
	}
	for _, ext := range markdownExts {
		// do not clobber an explicit user choice; only claim the extension when
		// it is unset or already ours
		cur := regGetString(`Software\Classes\`+ext, "")
		if cur == "" || cur == progID {
			regSetKeyValue(`Software\Classes\`+ext, "", progID)
		}
	}
	regSetKeyValue(`Software\Classes\Applications\MdReader.exe\shell\open\command`, "", cmd)
	regSetKeyValue(`Software\Classes\Applications\MdReader.exe\FriendlyAppName`, "", "MdReader")
	for _, ext := range markdownExts {
		regSetKeyValue(`Software\Classes\Applications\MdReader.exe\SupportedTypes\`+ext, "", "")
	}
	pSHChangeNotify.Call(SHCNE_ASSOCCHANGED, SHCNF_IDLIST, 0, 0)
	return nil
}

// removeAssoc unregisters the reader, leaving other applications untouched.
func removeAssoc() error {
	for _, ext := range markdownExts {
		if regGetString(`Software\Classes\`+ext, "") == progID {
			sp, err := syscall.UTF16PtrFromString(`Software\Classes\` + ext)
			if err == nil {
				h, e := regOpenKey(`Software\Classes\` + ext)
				if e == nil {
					np, _ := syscall.UTF16PtrFromString("")
					pRegDeleteValueW.Call(h, uintptr(unsafe.Pointer(np)))
					pRegCloseKey.Call(h)
				}
				_ = sp
			}
		}
	}
	regDeleteKey(`Software\Classes\` + progID)
	regDeleteKey(`Software\Classes\Applications\MdReader.exe`)
	pSHChangeNotify.Call(SHCNE_ASSOCCHANGED, SHCNF_IDLIST, 0, 0)
	return nil
}

func regOpenKey(sub string) (uintptr, error) {
	var h uintptr
	sp, err := syscall.UTF16PtrFromString(sub)
	if err != nil {
		return 0, err
	}
	r, _, e := advapi32.NewProc("RegOpenKeyExW").Call(HKEY_CURRENT_USER,
		uintptr(unsafe.Pointer(sp)), 0, KEY_ALL_ACCESS, uintptr(unsafe.Pointer(&h)))
	if r != 0 {
		return 0, errors.New("RegOpenKeyEx: " + e.Error())
	}
	return h, nil
}

// openWithDialog opens the Windows "Open with" picker for the current document
// (or for the executable when no document is open).
func (a *App) openWithDialog() {
	exe := exePath()
	target := a.path
	if target == "" {
		target = exe
	}
	params := "shell32.dll,OpenAs_RunDLL " + quoteArgs(target)
	shellOpen("rundll32.exe", params)
	_ = exe
}

func quoteArgs(s string) string {
	if !strings.ContainsAny(s, " \t") {
		return s
	}
	return `"` + s + `"`
}
