//go:build windows

// MdReader - a tiny portable Markdown reader/editor for Windows.
//
// Rendering goes through RichEdit (Msftedit.dll) so the executable stays small
// and dependency free: the markdown is converted to RTF by the local md package.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"mdreader/md"
)

const (
	appVersion = "1.0.3"
	wndClass   = "MdReaderMainWindow"
	editClass  = "RICHEDIT50W"

	wmSetText = 0x000C
	wmEraseBkgnd = 0x0014
	wmGetMinMaxInfo = 0x0024

	emSetModify      = 0x00B9
	emEmptyUndoBuf   = 0x00CD
	emLineFromChar   = 0x00C9
	emGetLineCount   = 0x00BA
	emGetFirstLine   = 0x00CE
	emLineScroll     = 0x00B6

	mkControl = 0x0008
)

// menu / command ids
const (
	cmOpen           = 100
	cmSave           = 101
	cmReload         = 102
	cmToggleEdit     = 103
	cmExit           = 104
	cmRecentClear    = 105
	cmRecentFirst    = 200
	cmRecentLast     = 209
	cmThemeAuto      = 300
	cmThemeLight     = 301
	cmThemeDark      = 302
	cmZoomIn         = 303
	cmZoomOut        = 304
	cmZoomReset      = 305
	cmSetDefault     = 306
	cmOpenWithDialog = 307
	cmRemoveAssoc    = 308
	cmCopyText       = 309
	cmAbout          = 310
	cmShortcuts      = 311
	cmFullscreen     = 312
	cmEscape         = 313
)

// ------------------------------------------------------------------- state

type App struct {
	padPx int // marge intérieure courante (pixels)
	streamAllowed bool // EM_STREAMIN activé explicitement (il peut tuer le process)
	hwnd   HWND
	edit   HWND
	status HWND
	menu   HMENU

	path    string
	raw     string // markdown source, LF normalised
	bom     bool
	eol     string
	dirty   bool
	inEdit  bool

	themeMode int // 0 auto, 1 light, 2 dark
	dark      bool
	scale     float64

	mirror     string
	links      []md.LinkSpan
	ranges     []linkRange
	linkRanges []linkRange

	cfg      Config
	cfgPath  string
	timer    uintptr
	accel    uintptr
	busy     bool
	hasTable bool
	lastW    int
	widthPx  int
	bgBrush  uintptr

	welcome bool

	recentMenu HMENU
	fullscreen bool
	savedRect  RECT
}

type linkRange struct {
	Start, End int32
	URL        string
	Local      bool
}

var app = &App{scale: 1.0}

// ---------------------------------------------------------------- streaming

var (
	gStream     []byte
	gStreamPos  int
	streamInCb  uintptr
	wndProcCb   uintptr
)

func streamInCB(cookie uintptr, buf uintptr, cb uint32, pcb uintptr) uintptr {
	n := len(gStream) - gStreamPos
	if n > int(cb) {
		n = int(cb)
	}
	if n > 0 {
		dst := unsafe.Slice((*byte)(unsafe.Pointer(buf)), int(cb))
		copy(dst, gStream[gStreamPos:gStreamPos+n])
		gStreamPos += n
	}
	*(*uint32)(unsafe.Pointer(pcb)) = uint32(n)
	return 0
}

func streamOutCB(cookie uintptr, buf uintptr, cb uint32, pcb uintptr) uintptr {
	n := int(cb)
	src := unsafe.Slice((*byte)(unsafe.Pointer(buf)), n)
	gStream = append(gStream, src...)
	*(*uint32)(unsafe.Pointer(pcb)) = uint32(n)
	return 0
}

// ------------------------------------------------------------------- main

func main() {
	runtime.LockOSThread()
	initDiag()
	defer func() {
		if r := recover(); r != nil {
			handlePanic("main", r)
		}
	}()

	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dump-rtf":
			if i+2 < len(args) {
				dumpRTF(args[i+1], args[i+2], contains(args[i:], "--dark"))
			}
			return
		case "--stream":
			app.streamAllowed = true
			logf("EM_STREAMIN activé par --stream")
		case "--selftest":
			selftest()
			return
		case "--help", "-h", "/?":
			help(args)
			return
		}
	}

	step("DPI + contrôles communs")
	enableDpiAwareness()
	initCommonControls()

	step("chargement de la configuration")
	app.cfg = loadConfig(&app.cfgPath)
	app.themeMode = app.cfg.Theme
	if app.scale = app.cfg.Scale; app.scale <= 0.3 || app.scale > 4 {
		app.scale = 1
	}
	app.dark = resolveDark(app.themeMode)

	step("enregistrement de la classe de fenêtre")
	registerClass()
	step("création de la fenêtre")
	app.createWindow()
	applyDarkChrome(app.dark)

	step("construction des menus")
	app.buildMenu()
	step("raccourcis clavier")
	app.buildAccel()

	// show the window before loading anything: the user must see the app come
	// up even if the document rendering fails
	step("affichage de la fenêtre")
	if app.cfg.Maximized {
		pShowWindow.Call(app.hwnd, 3 /*SW_SHOWMAXIMIZED*/)
	} else {
		pShowWindow.Call(app.hwnd, SW_SHOW)
	}
	pUpdateWindow.Call(app.hwnd)

	var file string
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			file = a
			break
		}
	}
	if file != "" {
		step("ouverture de " + file)
		app.openPath(file)
	} else {
		step("écran d'accueil")
		app.showWelcome()
	}

	step("boucle de messages")
	pSetTimer.Call(app.hwnd, 1, 1500, 0)
	logf("STARTUP OK")
	app.messageLoop()
	app.saveGeometry()
	saveConfig(app.cfgPath, app.cfg)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func (a *App) messageLoop() {
	var msg MSG
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		if a.accel != 0 {
			if t, _, _ := pTranslateAcceleratorW.Call(a.hwnd, a.accel, uintptr(unsafe.Pointer(&msg))); t != 0 {
				continue
			}
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func enableDpiAwareness() {
	if pSetProcessDpiAwarenessCtx.Find() == nil {
		// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4
		if r, _, _ := pSetProcessDpiAwarenessCtx.Call(^uintptr(3)); r != 0 {
			return
		}
	}
	pSetProcessDPIAware.Call()
}

func initCommonControls() {
	icc := initCommonControlsEx{Size: uint32(unsafe.Sizeof(initCommonControlsEx{})), ICC: ICC_BAR_CLASSES}
	pInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))
}

func registerClass() {
	wc := WNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		Style:         0x0002 | 0x0001, // CS_HREDRAW | CS_VREDRAW
		LpfnWndProc:   wndProcCb,
		HInstance:     hInstance(),
		HCursor:       loadCursor(32512), // IDC_ARROW
		LpszClassName: utf16Ptr(wndClass),
	}
	if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		fatal(fmt.Sprintf("RegisterClassExW a échoué: %v", err))
	}
}

func hInstance() HINSTANCE {
	h, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	return h
}

func i32(v int32) uintptr { return uintptr(uint32(v)) }

func loadCursor(id uint32) HCURSOR {
	r, _, _ := pLoadCursorW.Call(0, uintptr(id))
	return r
}

func (a *App) createWindow() {
	title := "MdReader"
	// CW_USEDEFAULT is 0x80000000 (INT_MIN) on the wire
	x, y, w, h := int32(-2147483648), int32(-2147483648), int32(1000), int32(760)
	if a.cfg.W > 300 && a.cfg.H > 200 {
		x, y, w, h = int32(a.cfg.X), int32(a.cfg.Y), int32(a.cfg.W), int32(a.cfg.H)
	}
	r, _, err := pCreateWindowExW.Call(
		WS_EX_ACCEPTFILES,
		uintptr(unsafe.Pointer(utf16Ptr(wndClass))),
		uintptr(unsafe.Pointer(utf16Ptr(title))),
		WS_OVERLAPPEDWINDOW|WS_CLIPCHILDREN,
		i32(x), i32(y), uintptr(w), uintptr(h),
		0, 0, hInstance(), 0)
	if r == 0 {
		fatal(fmt.Sprintf("CreateWindowExW a échoué: %v", err))
	}
	a.hwnd = r
}

func (a *App) createChildren() {
	// RichEdit
	h := loadLibrary("Msftedit.dll")
	logf("Msftedit.dll -> %#x", h)
	class := editClass
	if h == 0 {
		h = loadLibrary("Riched20.dll")
		class = "RichEdit20W"
	}
	if h == 0 {
		fatal("Impossible de charger le contrôle RichEdit (Msftedit.dll).")
	}
	r, _, err := pCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(utf16Ptr(class))),
		0,
		WS_CHILD|WS_VISIBLE|WS_VSCROLL|WS_TABSTOP|ES_MULTILINE|ES_NOHIDESEL|ES_AUTOVSCROLL,
		0, 0, 10, 10, a.hwnd, 0, h, 0)
	if r == 0 {
		fatal(fmt.Sprintf("Création du contrôle d'édition impossible: %v", err))
	}
	a.edit = r
	logf("RichEdit -> %#x", r)
	pSendMessageW.Call(a.edit, emEmptyUndoBuf, 0, 0)
	pSendMessageW.Call(a.edit, 0x0435 /*EM_EXLIMITTEXT*/, 0, 0x7FFFFFF0)
	pSendMessageW.Call(a.edit, 0x0452 /*EM_SETUNDOLIMIT*/, 1000, 0)
	pSendMessageW.Call(a.edit, EM_SETEVENTMASK, 0, ENM_LINK)

	// status bar
	sr, _, _ := pCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(utf16Ptr("msctls_statusbar32"))),
		0,
		WS_CHILD|WS_VISIBLE,
		0, 0, 10, 10, a.hwnd, 0, hInstance(), 0)
	a.status = sr
	parts := [2]int32{170, -1}
	pSendMessageW.Call(a.status, SB_SETPARTS, 2, uintptr(unsafe.Pointer(&parts[0])))

	pDragAcceptFiles.Call(a.hwnd, 1)
	a.makeBrush()
}

func loadLibrary(name string) uintptr {
	p, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0
	}
	r, _, _ := pLoadLibraryW.Call(uintptr(unsafe.Pointer(p)))
	return r
}

func fatal(msg string) {
	fatalDialog("fatal", msg)
	os.Exit(1)
}

func (a *App) msgBox(text, caption string, flags uintptr) int {
	r, _, _ := pMessageBoxW.Call(a.hwnd, uintptr(unsafe.Pointer(utf16Ptr(text))),
		uintptr(unsafe.Pointer(utf16Ptr(caption))), flags)
	return int(r)
}

// -------------------------------------------------------------- window proc

func (a *App) wndProc(hwnd HWND, msg uint32, wparam WPARAM, lparam LPARAM) uintptr {
	switch msg {
	case WM_CREATE:
		a.hwnd = hwnd
		a.createChildren()
		return 0
	case WM_SIZE:
		a.layout()
		return 0
	case wmGetMinMaxInfo:
		mmi := (*minMaxInfo)(unsafe.Pointer(lparam))
		mmi.PtMinTrackSize.X = 420
		mmi.PtMinTrackSize.Y = 320
		return 0
	case wmEraseBkgnd:
		a.eraseBackground(wparam)
		return 1
	case WM_SETFOCUS:
		pSetFocus.Call(a.edit)
		return 0
	case WM_COMMAND:
		a.onCommand(uint16(loWord(wparam)), uint16(hiWord(wparam)), lparam)
		return 0
	case WM_NOTIFY:
		if a.onNotify(lparam) {
			return 0
		}
	case WM_DROPFILES:
		a.onDrop(wparam)
		return 0
	case WM_MOUSEWHEEL:
		if uintptr(loWord(wparam))&mkControl != 0 {
			d := int32(int16(hiWord(wparam)))
			if d > 0 {
				a.zoom(1)
			} else {
				a.zoom(-1)
			}
			return 0
		}
	case WM_TIMER:
		a.pollFile()
		return 0
	case WM_INITMENUPOPUP:
		a.updateMenuState(HMENU(wparam))
		return 0
	case WM_CLOSE:
		if a.confirmDiscard() != nil {
			return 0
		}
		pDestroyWindow.Call(hwnd)
		return 0
	case WM_DESTROY:
		pKillTimer.Call(hwnd, 1)
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wparam, lparam)
	return r
}

type minMaxInfo struct {
	PtReserved     POINT
	PtMaxSize      POINT
	PtMaxPosition  POINT
	PtMinTrackSize POINT
	PtMaxTrackSize POINT
}

func (a *App) onNotify(lparam LPARAM) bool {
	n := (*enLink)(unsafe.Pointer(lparam))
	if n.Code != EN_LINK {
		return false
	}
	if n.Msg == WM_LBUTTONUP {
		a.activateLink(n.ChrgMin)
	}
	return true
}

func (a *App) activateLink(pos int32) {
	for _, r := range a.linkRanges {
		if pos >= r.Start && pos < r.End {
			a.openTarget(r.URL, r.Local)
			return
		}
	}
}

// ------------------------------------------------------------------ layout

func (a *App) dpi() int {
	if pGetDpiForWindow.Find() == nil {
		if r, _, _ := pGetDpiForWindow.Call(a.hwnd); r > 0 {
			return int(r)
		}
	}
	return 96
}

func (a *App) px(v int) int { return v * a.dpi() / 96 }

func (a *App) layout() {
	var rc RECT
	pGetClientRect.Call(a.hwnd, uintptr(unsafe.Pointer(&rc)))
	pSendMessageW.Call(a.status, WM_SIZE, 0, 0)
	var sr RECT
	pGetWindowRect.Call(a.status, uintptr(unsafe.Pointer(&sr)))
	sh := int(sr.Bottom - sr.Top)
	pad := a.px(18)
	w := int(rc.Right-rc.Left) - 2*pad
	h := int(rc.Bottom-rc.Top) - sh - 2*pad
	if w < 50 {
		w = 50
	}
	if h < 50 {
		h = 50
	}
	pMoveWindow.Call(a.edit, uintptr(pad), uintptr(pad), uintptr(w), uintptr(h), 1)
	a.widthPx = int(rc.Right - rc.Left)
	if a.hasTable && !a.inEdit && abs(a.lastW-a.widthPx) > 48 {
		a.lastW = a.widthPx
		a.renderRead()
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (a *App) makeBrush() {
	if a.bgBrush != 0 {
		gdi32.NewProc("DeleteObject").Call(a.bgBrush)
		a.bgBrush = 0
	}
	t := a.theme()
	r, _, _ := gdi32.NewProc("CreateSolidBrush").Call(uintptr(colorRef(t.BG)))
	a.bgBrush = r
}

func (a *App) eraseBackground(hdc uintptr) {
	var rc RECT
	pGetClientRect.Call(a.hwnd, uintptr(unsafe.Pointer(&rc)))
	if a.bgBrush == 0 {
		a.makeBrush()
	}
	user32.NewProc("FillRect").Call(hdc, uintptr(unsafe.Pointer(&rc)), a.bgBrush)
}

func colorRef(hex string) uint32 {
	hex = strings.TrimPrefix(hex, "#")
	var r, g, b uint32
	if len(hex) == 6 {
		fmt.Sscanf(hex[0:2], "%02x", &r)
		fmt.Sscanf(hex[2:4], "%02x", &g)
		fmt.Sscanf(hex[4:6], "%02x", &b)
	}
	return r | g<<8 | b<<16
}

// ------------------------------------------------------------------ themes

func (a *App) theme() md.Theme {
	if a.dark {
		return md.DarkTheme()
	}
	return md.LightTheme()
}

func resolveDark(mode int) bool {
	switch mode {
	case 1:
		return false
	case 2:
		return true
	}
	return systemDark()
}

func (a *App) setThemeMode(mode int) {
	a.themeMode = mode
	a.dark = resolveDark(mode)
	a.cfg.Theme = mode
	a.makeBrush()
	a.applyColors()
	if a.inEdit {
		a.applyEditorFormat()
	} else {
		a.renderRead()
	}
}

func (a *App) applyColors() {
	if a.edit == 0 {
		return
	}
	pSendMessageW.Call(a.edit, EM_SETBKGNDCOLOR, 0, uintptr(colorRef(a.theme().BG)))
	invalidate(a.hwnd)
}

// ------------------------------------------------------------ thème système

// applyDarkChrome asks Windows for dark menus and title bar (Undocumented but
// widespread trick: SetPreferredAppMode in uxtheme). Every call is optional and
// silently ignored when the running Windows does not provide it.
func applyDarkChrome(dark bool) {
	if !dark {
		return
	}
	uxtheme := syscall.NewLazyDLL("uxtheme.dll")
	mode := uintptr(2) // AllowDark
	if h, _, _ := kernel32.NewProc("GetModuleHandleW").Call(uintptr(unsafe.Pointer(utf16Ptr("uxtheme.dll")))); h != 0 {
		if pGa, _, _ := kernel32.NewProc("GetProcAddress").Call(h, 135); pGa != 0 {
			syscall.SyscallN(pGa, mode)
		}
		if pGa, _, _ := kernel32.NewProc("GetProcAddress").Call(h, 136); pGa != 0 {
			syscall.SyscallN(pGa)
		}
	}
	if app.status != 0 {
		if p := uxtheme.NewProc("SetWindowTheme"); p.Find() == nil {
			p.Call(app.status, uintptr(unsafe.Pointer(utf16Ptr("DarkMode_Explorer"))), 0)
		}
	}
}

func invalidate(h HWND) {
	user32.NewProc("InvalidateRect").Call(h, 0, 1)
}

// -------------------------------------------------------------- documents

func (a *App) openPath(p string) {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	if err := a.confirmDiscard(); err != nil {
		return
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		a.msgBox("Impossible d'ouvrir le fichier:\n"+err.Error(), "MdReader", MB_OK|MB_ICONERROR)
		return
	}
	a.path = abs
	a.welcome = false
	a.raw, a.bom, a.eol = decodeDocument(data)
	a.dirty = false
	a.updateRecent(abs)
	a.setTitle()
	if a.inEdit {
		a.setEditorText()
	} else {
		a.renderRead()
	}
	a.updateStatus("")
	if a.raw != "" {
		pSendMessageW.Call(a.edit, 0x00B1 /*EM_SETSEL*/, 0, 0)
		pSendMessageW.Call(a.edit, emLineScroll, 0, 0)
	}
}

func (a *App) showWelcome() {
	a.welcome = true
	a.path = ""
	a.dirty = false
	a.raw = welcomeMarkdown()
	a.inEdit = false
	a.renderRead()
	a.updateStatus("Aucun fichier ouvert — Ctrl+O pour ouvrir un .md")
	a.setTitle()
}

func welcomeMarkdown() string {
	return "# MdReader\n\n" +
		"Lecteur Markdown portable. Aucun fichier ouvert pour le moment.\n\n" +
		"## Raccourcis\n\n" +
		"- **Ctrl+O** — ouvrir un fichier\n" +
		"- **Ctrl+E** — basculer lecture / édition\n" +
		"- **Ctrl+S** — enregistrer (en mode édition)\n" +
		"- **F5** — recharger depuis le disque\n" +
		"- **Ctrl + molette** — zoom\n" +
		"- **F11** — plein écran\n\n" +
		"## Association .md\n\n" +
		"Menu **Fichier → Définir comme lecteur .md par défaut** pour que Windows ouvre " +
		"les fichiers markdown avec cette application (double-clic depuis l'explorateur).\n\n" +
		"> Les fichiers modifiés sur le disque sont rechargés automatiquement tant que " +
		"le mode édition n'a pas de modifications en attente.\n\n" +
		"| Élément | État |\n|---|---|\n| Titres, listes, citations | oui |\n| Tableaux, cases à cocher | oui |\n| Liens cliquables | oui |\n\n" +
		"```text\n" +
		"Les blocs de code s'affichent en monospace sur fond grisé.\n" +
		"```\n"
}

func decodeDocument(data []byte) (text string, bom bool, eol string) {
	eol = "\n"
	if strings.Contains(string(data), "\r\n") {
		eol = "\r\n"
	}
	switch {
	case len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF:
		text, bom = string(data[3:]), true
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE:
		text = decodeUTF16(data[2:], true)
		bom = true
	case len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF:
		text = decodeUTF16(data[2:], false)
		bom = true
	default:
		text = string(data)
	}
	if !utf8.ValidString(text) {
		text = fixUTF8(text)
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text, bom, eol
}

func decodeUTF16(b []byte, little bool) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		if little {
			u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
		} else {
			u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
		}
	}
	return string(utf16.Decode(u))
}

func fixUTF8(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b.WriteRune('\uFFFD')
			i++
			continue
		}
		b.WriteString(s[i : i+size])
		i += size
	}
	return b.String()
}

func encodeDocument(text string, bom bool, eol string) []byte {
	if eol != "\n" {
		text = strings.ReplaceAll(text, "\n", eol)
	}
	out := []byte(text)
	if bom {
		out = append([]byte{0xEF, 0xBB, 0xBF}, out...)
	}
	return out
}

// ------------------------------------------------------------------ render

func (a *App) renderRead() {
	if a.edit == 0 {
		return
	}
	t := a.theme()
	w := a.widthPx
	if w <= 0 {
		w = a.px(900)
	}
	// keep a comfortable reading measure: on a wide window the text is a
	// centred column instead of edge-to-edge lines
	side := a.px(18)
	a.padPx = side
	maxCol := a.px(820)
	if w-2*side > maxCol {
		side = (w - maxCol) / 2
	}
	colPx := w - 2*side - a.px(20)
	if colPx < a.px(320) {
		colPx = a.px(320)
		side = a.px(18)
	}
	a.padPx = side
	tw := int(float64(colPx) * 1440 / float64(a.dpi()))
	padTw := int(float64(side) * 1440 / float64(a.dpi()))
	res, ok := safeRender(a.raw, md.Options{Theme: t, Scale: a.scale, WidthTwips: tw, PadTwips: padTw})
	if !ok {
		res = md.Result{Text: a.raw}
	}
	a.mirror = res.Text
	a.links = res.Links
	a.hasTable = strings.Contains(res.RTF, `\trowd`)

	a.busy = true
	a.setReadOnly(true)
	// the background must follow the theme in every display mode: without
	// this, a dark-theme text colour would land on the default white control
	a.applyColors()

	mode := a.loadRich(res.RTF)
	// the rich rendering carries its own margins inside the RTF, the plain
	// fallback needs them set on the control
	if mode == rmPlain {
		a.applyMargins(a.padPx)
	} else {
		a.applyMargins(a.px(6))
	}
	switch mode {
	case rmSetTextEx, rmStream:
		pSendMessageW.Call(a.edit, emSetModify, 0, 0)
		pSendMessageW.Call(a.edit, emEmptyUndoBuf, 0, 0)
		a.applyColors()
		a.applyLinks()
	default:
		// no rich rendering available: stay usable by showing the readable
		// mirror of the document instead of the raw markdown
		text := a.mirror
		if text == "" {
			text = a.raw
		}
		a.setBodyText(text)
		a.updateStatus("Rendu enrichi indisponible — texte brut (voir mdreader-log.txt)")
	}
	a.busy = false
	logf("renderRead terminé (rtf=%d octets)", len(res.RTF))
}

// render modes, from the least risky to the most risky
const (
	rmPlain     = 0 // plain text, always works
	rmSetTextEx = 3 // EM_SETTEXTEX with RTF: no callback into Go
	rmStream    = 2 // EM_STREAMIN: needs a Go callback
)

// setTextEx is the SETTEXTEX structure used by EM_SETTEXTEX.
type setTextEx struct {
	Flags    uint32
	Codepage uint32
}

// loadRich tries every way of getting the rich text into the control and
// remembers the one that works, so a method that kills the process on this
// machine is never tried twice.
func (a *App) loadRich(rtf string) int {
	if rtf == "" {
		return rmPlain
	}
	// One method only, tried once: repeatable behaviour matters more than
	// cleverness here. EM_SETTEXTEX cannot kill the process (no callback into
	// Go), so either it works or the readable plain text is shown.
	if a.trySetTextEx(rtf) {
		return rmSetTextEx
	}
	if a.streamAllowed && a.streamRTF(rtf) {
		return rmStream
	}
	return rmPlain
}

// trySetTextEx pushes the RTF through EM_SETTEXTEX, which is the documented way
// of handing RTF to the control as a string: the control reads it with its RTF
// reader when the text starts with {\rtf. No callback into Go is involved, so
// this cannot hit the runtime bug that EM_STREAMIN can.
func (a *App) trySetTextEx(rtf string) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			logf("PANIC pendant EM_SETTEXTEX: %v", r)
			ok = false
		}
	}()
	// the RTF stream is pure ASCII (every non-ASCII character is written as a
	// \uNNNN? escape), so it can be handed over as an ANSI string
	buf := append([]byte(rtf), 0)
	ste := setTextEx{Flags: 0 /* ST_DEFAULT */, Codepage: 1252}
	r, _, _ := pSendMessageW.Call(a.edit, EM_SETTEXTEX,
		uintptr(unsafe.Pointer(&ste)), uintptr(unsafe.Pointer(&buf[0])))
	// the return value is the documented success signal for a whole-text set
	logf("EM_SETTEXTEX -> %d (%d caractères dans le contrôle)", r, len(a.controlText()))
	return r == 1
}

// renderMarker is created before streaming and removed afterwards: if it is
// still there at startup, the previous run died inside the rich rendering.
func renderMarkerPath() string {
	return filepath.Join(filepath.Dir(logPath), "mdreader-render.flag")
}

// streamRTF pushes the RTF into the control. It reports false when the control
// stays empty, which is what happens when the stream could not be parsed.
func (a *App) streamRTF(rtf string) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			logf("PANIC pendant EM_STREAMIN: %v\n%s", r, debug.Stack())
			ok = false
		}
	}()
	// the marker is removed on success: if the process dies here, it stays
	// behind and the next run avoids this method
	os.WriteFile(renderMarkerPath(), []byte("streaming"), 0o644)
	gStream = []byte(rtf)
	gStreamPos = 0
	es := edStream{PfnCallback: streamInCb}
	r, _, _ := pSendMessageW.Call(a.edit, EM_STREAMIN, SF_RTF, uintptr(unsafe.Pointer(&es)))
	gStream = nil
	gStreamPos = 0
	logf("EM_STREAMIN -> %d, dwError=%d", r, es.Error)
	txt := a.controlText()
	if len(rtf) > 200 && len(txt) < 2 {
		logf("EM_STREAMIN: contrôle vide, méthode abandonnée")
		return false
	}
	head := txt
	if len(head) > 40 {
		head = head[:40]
	}
	if strings.Contains(head, `{\rtf`) {
		logf("EM_STREAMIN: RTF non interprété, méthode abandonnée")
		return false
	}
	os.Remove(renderMarkerPath())
	return true
}

func (a *App) setReadOnly(on bool) {
	v := uintptr(0)
	if on {
		v = 1
	}
	pSendMessageW.Call(a.edit, EM_SETREADONLY, v, 0)
}

func (a *App) setEditorText() {
	a.setEditorTextValue(a.raw)
	a.applyEditorFormat()
}

// setBodyText displays readable prose (the plain-text fallback) in the UI font
// rather than in the monospace font used for the markdown source.
func (a *App) setBodyText(value string) {
	a.setEditorTextValue(value)
	a.applyBodyFormat()
}

// setEditorTextValue displays arbitrary text (used by the plain-text fallback,
// which shows the readable mirror rather than the markdown source).
func (a *App) setEditorTextValue(value string) {
	a.busy = true
	txt := strings.ReplaceAll(value, "\n", "\r")
	pSendMessageW.Call(a.edit, wmSetText, 0, uintptr(unsafe.Pointer(utf16Ptr(txt))))
	pSendMessageW.Call(a.edit, emSetModify, 0, 0)
	pSendMessageW.Call(a.edit, emEmptyUndoBuf, 0, 0)
	a.busy = false
}

// applyBodyFormat sets the proportional reading font.
func (a *App) applyBodyFormat() {
	t := a.theme()
	var cf charFormat2W
	cf.CbSize = uint32(unsafe.Sizeof(cf))
	cf.DwMask = CFM_FACE | CFM_SIZE | CFM_COLOR | CFM_BOLD
	cf.DwEffects = 0
	cf.YHeight = int32(10 * 22 * a.scale)
	cf.CrTextColor = colorRef(t.Text)
	face := utf16.Encode([]rune("Segoe UI"))
	for i := 0; i < len(face) && i < len(cf.SzFaceName); i++ {
		cf.SzFaceName[i] = face[i]
	}
	pSendMessageW.Call(a.edit, EM_SETCHARFORMAT, SCF_ALL, uintptr(unsafe.Pointer(&cf)))
}

const (
	emSetTextEx = 0x0461 // EM_SETTEXTEX
	emSetMargins = 0x00D3 // EM_SETMARGINS
)

// applyMargins sets the left/right inner margin of the control, in pixels.
func (a *App) applyMargins(px int) {
	if a.edit == 0 {
		return
	}
	lp := uintptr(px) | uintptr(px)<<16
	pSendMessageW.Call(a.edit, emSetMargins, EC_LEFTMARGIN|EC_RIGHTMARGIN, lp)
}

func (a *App) applyEditorFormat() {
	t := a.theme()
	var cf charFormat2W
	cf.CbSize = uint32(unsafe.Sizeof(cf))
	cf.DwMask = CFM_FACE | CFM_SIZE | CFM_COLOR | CFM_BOLD
	cf.DwEffects = 0
	cf.YHeight = int32(10 * 19 * a.scale)
	cf.CrTextColor = colorRef(t.Text)
	copy(cf.SzFaceName[:], utf16.Encode([]rune("Consolas")))
	pSendMessageW.Call(a.edit, EM_SETCHARFORMAT, SCF_ALL, uintptr(unsafe.Pointer(&cf)))
}

func (a *App) editorText() string {
	n, _, _ := pSendMessageW.Call(a.edit, EM_GETTEXTLENGTHEX,
		uintptr(unsafe.Pointer(&getTextLengthEx{Flags: GTL_NUMCHARS | GTL_PRECISE, Codepage: 1200})), 0)
	count := int(n) + 2
	if count < 4 {
		count = 4
	}
	buf := make([]uint16, count)
	gte := getTextEx{Cb: uint32(count * 2), Flags: 0, Codepage: 1200}
	pSendMessageW.Call(a.edit, EM_GETTEXTEX, uintptr(unsafe.Pointer(&gte)), uintptr(unsafe.Pointer(&buf[0])))
	runtime.KeepAlive(buf)
	s := syscall.UTF16ToString(buf)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return s
}

// --------------------------------------------------------------- links

func (a *App) applyLinks() {
	if len(a.links) == 0 {
		a.linkRanges = nil
		return
	}
	ctrl := a.controlText()
	a.linkRanges = reconcile(ctrl, a.mirror, a.links)
	if len(a.linkRanges) == 0 {
		return
	}
	pSendMessageW.Call(a.edit, EM_HIDESELECTION, 1, 0)
	for _, r := range a.linkRanges {
		cr := charRange{CpMin: r.Start, CpMax: r.End}
		pSendMessageW.Call(a.edit, EM_EXSETSEL, 0, uintptr(unsafe.Pointer(&cr)))
		var cf charFormat2W
		cf.CbSize = uint32(unsafe.Sizeof(cf))
		cf.DwMask = CFM_LINK
		cf.DwEffects = CFE_LINK
		pSendMessageW.Call(a.edit, EM_SETCHARFORMAT, SCF_SELECTION, uintptr(unsafe.Pointer(&cf)))
	}
	pSendMessageW.Call(a.edit, EM_HIDESELECTION, 0, 0)
	cr := charRange{}
	pSendMessageW.Call(a.edit, EM_EXSETSEL, 0, uintptr(unsafe.Pointer(&cr)))
}

func (a *App) controlText() string {
	n, _, _ := pSendMessageW.Call(a.edit, EM_GETTEXTLENGTHEX,
		uintptr(unsafe.Pointer(&getTextLengthEx{Flags: GTL_NUMCHARS | GTL_PRECISE, Codepage: 1200})), 0)
	count := int(n) + 2
	if count < 4 {
		count = 4
	}
	buf := make([]uint16, count)
	gte := getTextEx{Cb: uint32(count * 2), Flags: 0, Codepage: 1200}
	pSendMessageW.Call(a.edit, EM_GETTEXTEX, uintptr(unsafe.Pointer(&gte)), uintptr(unsafe.Pointer(&buf[0])))
	runtime.KeepAlive(buf)
	return syscall.UTF16ToString(buf)
}

// reconcile maps link spans from the rendered mirror onto the character
// positions of the actual control text. When both are identical the mapping is
// direct; otherwise a small alignment walk absorbs the differences (RichEdit
// inserts extra markers for table cells).
func reconcile(ctrl, mirror string, spans []md.LinkSpan) []linkRange {
	if ctrl == "" || mirror == "" {
		return nil
	}
	if ctrl == mirror {
		out := make([]linkRange, 0, len(spans))
		for _, s := range spans {
			out = append(out, linkRange{int32(s.Start), int32(s.End), s.URL, s.Local})
		}
		return out
	}
	c := []rune(ctrl)
	mr := []rune(mirror)
	mp := make([]int, len(mr)+1)
	for i := range mp {
		mp[i] = -1
	}
	i, j := 0, 0
	for i < len(c) && j < len(mr) {
		if c[i] == mr[j] {
			mp[j] = i
			i++
			j++
			continue
		}
		adv := false
		for k := 1; k <= 4 && i+k < len(c); k++ {
			if c[i+k] == mr[j] {
				i += k
				adv = true
				break
			}
		}
		if adv {
			continue
		}
		for k := 1; k <= 4 && j+k < len(mr); k++ {
			if mr[j+k] == c[i] {
				j += k
				adv = true
				break
			}
		}
		if adv {
			continue
		}
		i++
		j++
	}
	out := make([]linkRange, 0, len(spans))
	for _, s := range spans {
		if s.Start >= len(mp) || s.End > len(mr) {
			continue
		}
		start, end := -1, -1
		for k := s.Start; k < s.End && k < len(mp); k++ {
			if mp[k] >= 0 {
				start = mp[k]
				break
			}
		}
		for k := s.End - 1; k >= s.Start && k < len(mp); k-- {
			if mp[k] >= 0 {
				end = mp[k] + 1
				break
			}
		}
		if start >= 0 && end > start {
			out = append(out, linkRange{int32(start), int32(end), s.URL, s.Local})
		}
	}
	return out
}

func (a *App) openTarget(url string, local bool) {
	if strings.HasPrefix(url, "#") {
		return
	}
	if !local {
		shellOpen(url, "")
		return
	}
	base := filepath.Dir(a.path)
	p := url
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, url)
	}
	ext := strings.ToLower(filepath.Ext(p))
	if ext == ".md" || ext == ".markdown" || ext == ".mdown" || ext == ".mkd" || ext == ".txt" {
		a.openPath(p)
		return
	}
	if _, err := os.Stat(p); err != nil {
		a.updateStatus("Fichier introuvable: " + p)
		return
	}
	shellOpen(p, "")
}

func shellOpen(file, params string) {
	var pp uintptr
	if params != "" {
		pp = uintptr(unsafe.Pointer(utf16Ptr(params)))
	}
	r, _, _ := pShellExecuteW.Call(0,
		uintptr(unsafe.Pointer(utf16Ptr("open"))),
		uintptr(unsafe.Pointer(utf16Ptr(file))),
		pp, 0, SW_SHOW)
	if r <= 32 {
		pMessageBoxW.Call(0, uintptr(unsafe.Pointer(utf16Ptr("Impossible d'ouvrir :\n"+file))),
			uintptr(unsafe.Pointer(utf16Ptr("MdReader"))), MB_OK|MB_ICONERROR)
	}
}

// ------------------------------------------------------------------ editing

func (a *App) toggleEdit() {
	if a.inEdit {
		// back to reading: keep whatever the user typed in memory
		a.raw = a.editorText()
		a.inEdit = false
		a.renderRead()
		if a.dirty {
			a.setTitle()
		}
	} else {
		a.inEdit = true
		a.setReadOnly(false)
		a.setEditorText()
		a.applyColors()
	}
	a.updateStatus("")
	a.setTitle()
	pSetFocus.Call(a.edit)
}

func (a *App) save() bool {
	if !a.inEdit {
		return true
	}
	if a.path == "" {
		if !a.saveAs() {
			return false
		}
		return true
	}
	a.raw = a.editorText()
	data := encodeDocument(a.raw, a.bom, a.eol)
	if err := os.WriteFile(a.path, data, 0o644); err != nil {
		a.msgBox("Enregistrement impossible:\n"+err.Error(), "MdReader", MB_OK|MB_ICONERROR)
		return false
	}
	a.dirty = false
	pSendMessageW.Call(a.edit, emSetModify, 0, 0)
	a.updateStatus("Enregistré")
	a.setTitle()
	return true
}

func (a *App) saveAs() bool {
	p, ok := a.fileDialog(true)
	if !ok {
		return false
	}
	a.path = p
	a.raw = a.editorText()
	data := encodeDocument(a.raw, a.bom, a.eol)
	if err := os.WriteFile(a.path, data, 0o644); err != nil {
		a.msgBox("Enregistrement impossible:\n"+err.Error(), "MdReader", MB_OK|MB_ICONERROR)
		return false
	}
	a.dirty = false
	a.updateRecent(p)
	a.updateStatus("Enregistré")
	a.setTitle()
	return true
}

func (a *App) onEditChange() {
	if a.busy || !a.inEdit {
		return
	}
	if !a.dirty {
		a.dirty = true
		a.setTitle()
	}
}

func (a *App) confirmDiscard() error {
	if !a.dirty {
		return nil
	}
	r := a.msgBox("Le document contient des modifications non enregistrées.\n\nLes enregistrer ?",
		"MdReader", MB_YESNOCANCEL|MB_ICONWARNING)
	switch r {
	case IDYES:
		if a.save() {
			return nil
		}
		return fmt.Errorf("cancel")
	case IDNO:
		return nil
	}
	return fmt.Errorf("cancel")
}

func (a *App) fileDialog(save bool) (string, bool) {
	buf := make([]uint16, 4096)
	if a.path != "" {
		copy(buf, utf16.Encode([]rune(a.path)))
	}
	filter := "Fichiers Markdown\x00*.md;*.markdown;*.mdown;*.mkd\x00Tous les fichiers\x00*.*\x00\x00"
	f := syscall.StringToUTF16(filter)
	title := "Ouvrir un document Markdown"
	if save {
		title = "Enregistrer le document"
	}
	ofn := OPENFILENAMEW{
		StructSize:  uint32(unsafe.Sizeof(OPENFILENAMEW{})),
		Owner:       a.hwnd,
		Filter:      &f[0],
		File:        &buf[0],
		MaxFile:     uint32(len(buf)),
		FilterIndex: 1,
		Title:       utf16Ptr(title),
		Flags:      OFN_FILEMUSTEXIST | OFN_PATHMUSTEXIST | OFN_HIDEREADONLY | OFN_EXPLORER,
	}
	if save {
		ofn.Flags = OFN_PATHMUSTEXIST | OFN_HIDEREADONLY | OFN_EXPLORER
		ofn.DefExt = utf16Ptr("md")
	}
	r, _, _ := pGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	runtime.KeepAlive(buf)
	runtime.KeepAlive(f)
	if r == 0 {
		return "", false
	}
	return syscall.UTF16ToString(buf), true
}

func (a *App) zoom(dir int) {
	f := 1.1
	if dir < 0 {
		f = 1 / 1.1
	}
	ns := a.scale * f
	if ns < 0.5 {
		ns = 0.5
	}
	if ns > 3 {
		ns = 3
	}
	if ns == a.scale {
		return
	}
	a.scale = ns
	a.cfg.Scale = ns
	a.applyZoom()
}

func (a *App) applyZoom() {
	if a.inEdit {
		a.applyEditorFormat()
		return
	}
	a.renderRead()
}

func (a *App) pollFile() {
	if a.path == "" || a.dirty || a.welcome {
		return
	}
	st, err := os.Stat(a.path)
	if err != nil {
		return
	}
	if st.ModTime().UnixNano() == a.cfg.lastMod {
		return
	}
	data, err := os.ReadFile(a.path)
	if err != nil {
		return
	}
	a.cfg.lastMod = st.ModTime().UnixNano()
	txt, bom, eol := decodeDocument(data)
	if txt == a.raw {
		return
	}
	a.raw, a.bom, a.eol = txt, bom, eol
	if a.inEdit {
		a.setEditorText()
	} else {
		a.renderRead()
	}
	a.updateStatus("Rechargé depuis le disque")
}

func (a *App) onDrop(hdrop uintptr) {
	buf := make([]uint16, 4096)
	r, _, _ := pDragQueryFileW.Call(hdrop, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	pDragFinish.Call(hdrop)
	runtime.KeepAlive(buf)
	if r > 0 {
		a.openPath(syscall.UTF16ToString(buf))
	}
}

// -------------------------------------------------------------------- menu

func (a *App) buildMenu() {
	file := mustMenu(pCreatePopupMenu.Call())
	edit := mustMenu(pCreatePopupMenu.Call())
	view := mustMenu(pCreatePopupMenu.Call())

	appendMenu(file, MF_STRING, uintptr(cmOpen), "&Ouvrir…\tCtrl+O")
	recent := mustMenu(pCreatePopupMenu.Call())
	a.recentMenu = recent
	a.fillRecentMenu()
	appendMenu(file, MF_POPUP, recent, "Fichiers &récents")
	appendMenu(file, MF_SEPARATOR, 0, "")
	appendMenu(file, MF_STRING, uintptr(cmToggleEdit), "&Éditer le markdown\tCtrl+E")
	appendMenu(file, MF_STRING, uintptr(cmSave), "&Enregistrer\tCtrl+S")
	appendMenu(file, MF_STRING, uintptr(cmReload), "&Recharger depuis le disque\tF5")
	appendMenu(file, MF_SEPARATOR, 0, "")
	appendMenu(file, MF_STRING, uintptr(cmCopyText), "Copier le &texte brut")
	appendMenu(file, MF_SEPARATOR, 0, "")
	appendMenu(file, MF_STRING, uintptr(cmSetDefault), "Définir comme lecteur .md par défaut")
	appendMenu(file, MF_STRING, uintptr(cmOpenWithDialog), "Choisir via « Ouvrir avec »…")
	appendMenu(file, MF_STRING, uintptr(cmRemoveAssoc), "Retirer l'association .md")
	appendMenu(file, MF_SEPARATOR, 0, "")
	appendMenu(file, MF_STRING, uintptr(cmExit), "&Quitter\tAlt+F4")

	appendMenu(edit, MF_STRING, uintptr(cmZoomOut), "Zoom -")
	appendMenu(edit, MF_STRING, uintptr(cmZoomIn), "Zoom +")
	appendMenu(edit, MF_STRING, uintptr(cmZoomReset), "Zoom 100 %")
	appendMenu(edit, MF_SEPARATOR, 0, "")
	appendMenu(edit, MF_STRING, uintptr(cmThemeAuto), "Thème : automatique")
	appendMenu(edit, MF_STRING, uintptr(cmThemeLight), "Thème : clair")
	appendMenu(edit, MF_STRING, uintptr(cmThemeDark), "Thème : sombre")

	appendMenu(view, MF_STRING, uintptr(cmShortcuts), "Raccourcis clavier")
	appendMenu(view, MF_STRING, uintptr(cmAbout), "À propos de MdReader")

	bar := mustMenu(pCreateMenu.Call())
	appendMenu(bar, MF_POPUP, file, "&Fichier")
	appendMenu(bar, MF_POPUP, edit, "&Affichage")
	appendMenu(bar, MF_POPUP, view, "&Aide")
	old := a.menu
	a.menu = bar
	pSetMenu.Call(a.hwnd, bar)
	if old != 0 {
		// destroy only once detached, otherwise the window keeps a dangling
		// menu handle
		pDestroyMenu.Call(old)
	}
}

func mustMenu(r uintptr, _ uintptr, _ error) HMENU { return r }

func appendMenu(h HMENU, flags uintptr, id uintptr, text string) {
	if flags&MF_SEPARATOR != 0 {
		pAppendMenuW.Call(h, MF_SEPARATOR, 0, 0)
		return
	}
	if flags&MF_POPUP != 0 {
		pAppendMenuW.Call(h, MF_POPUP, uintptr(id), uintptr(unsafe.Pointer(utf16Ptr(text))))
		return
	}
	pAppendMenuW.Call(h, MF_STRING, uintptr(id), uintptr(unsafe.Pointer(utf16Ptr(text))))
}

func (a *App) fillRecentMenu() {
	if a.recentMenu == 0 {
		return
	}
	n := len(a.cfg.Recent)
	for i := 0; i < n && i < 10; i++ {
		label := fmt.Sprintf("&%d %s", (i+1)%10, filepath.Base(a.cfg.Recent[i]))
		appendMenu(a.recentMenu, MF_STRING, uintptr(cmRecentFirst+i), label)
	}
	if n == 0 {
		appendMenu(a.recentMenu, MF_STRING, 0, "(vide)")
		pEnableMenuItem.Call(a.recentMenu, 0, MF_BYPOSITION|MF_GRAYED)
	} else {
		appendMenu(a.recentMenu, MF_SEPARATOR, 0, "")
		appendMenu(a.recentMenu, MF_STRING, cmRecentClear, "Vider la liste")
	}
}

func (a *App) updateRecent(p string) {
	list := []string{p}
	for _, v := range a.cfg.Recent {
		if !strings.EqualFold(v, p) {
			list = append(list, v)
		}
	}
	if len(list) > 10 {
		list = list[:10]
	}
	a.cfg.Recent = list
	a.cfg.lastMod = 0
	if st, err := os.Stat(p); err == nil {
		a.cfg.lastMod = st.ModTime().UnixNano()
	}
	a.buildMenu()
}

func (a *App) updateMenuState(h HMENU) {
	check := func(id int, on bool) {
		v := uintptr(MF_BYCOMMAND)
		if on {
			v |= MF_CHECKED
		}
		pCheckMenuItem.Call(h, uintptr(id), v)
	}
	check(cmThemeAuto, a.themeMode == 0)
	check(cmThemeLight, a.themeMode == 1)
	check(cmThemeDark, a.themeMode == 2)
	check(cmToggleEdit, a.inEdit)
	en := uintptr(MF_BYCOMMAND)
	if !a.inEdit {
		en |= MF_GRAYED
	}
	pEnableMenuItem.Call(h, cmSave, en)
}

func (a *App) buildAccel() {
	if pCreateAcceleratorTableW.Find() != nil {
		return
	}
	list := []accel{
		{FVIRTKEY | FCONTROL, 'O', cmOpen},
		{FVIRTKEY | FCONTROL, 'E', cmToggleEdit},
		{FVIRTKEY | FCONTROL, 'S', cmSave},
		{FVIRTKEY, VK_F5, cmReload},
		{FVIRTKEY | FCONTROL, 0xBB, cmZoomIn},  // Ctrl+= (VK_OEM_PLUS)
		{FVIRTKEY | FCONTROL, 0x6B, cmZoomIn},   // Ctrl+num +
		{FVIRTKEY | FCONTROL, 0xBD, cmZoomOut},  // Ctrl+- (VK_OEM_MINUS)
		{FVIRTKEY | FCONTROL, 0x6D, cmZoomOut},  // Ctrl+num -
		{FVIRTKEY | FCONTROL, '0', cmZoomReset},
		{FVIRTKEY, 0x7A, cmFullscreen},
		{FVIRTKEY, 0x1B, cmEscape},
		{FVIRTKEY | FCONTROL, 'L', cmThemeLight},
		{FVIRTKEY | FCONTROL, 'D', cmThemeDark},
	}
	r, _, _ := pCreateAcceleratorTableW.Call(uintptr(unsafe.Pointer(&list[0])), uintptr(len(list)))
	a.accel = r
}

func (a *App) onCommand(id uint16, code uint16, lparam LPARAM) {
	if lparam != 0 {
		if HWND(lparam) == a.edit && code == EN_CHANGE {
			a.onEditChange()
		}
		return
	}
	switch int(id) {
	case cmOpen:
		if p, ok := a.fileDialog(false); ok {
			a.openPath(p)
		}
	case cmSave:
		a.save()
	case cmReload:
		a.reload()
	case cmToggleEdit:
		a.toggleEdit()
	case cmExit:
		if a.confirmDiscard() == nil {
			pDestroyWindow.Call(a.hwnd)
		}
	case cmRecentClear:
		a.cfg.Recent = nil
		a.buildMenu()
	case cmThemeAuto:
		a.setThemeMode(0)
	case cmThemeLight:
		a.setThemeMode(1)
	case cmThemeDark:
		a.setThemeMode(2)
	case cmZoomIn:
		a.zoom(1)
	case cmZoomOut:
		a.zoom(-1)
	case cmZoomReset:
		a.scale = 1
		a.cfg.Scale = 1
		a.applyZoom()
	case cmCopyText:
		a.copyPlainText()
	case cmSetDefault:
		if err := installAssoc(); err != nil {
			a.msgBox("Association impossible:\n"+err.Error(), "MdReader", MB_OK|MB_ICONERROR)
		} else {
			a.msgBox("MdReader est maintenant associé aux fichiers .md.\n\n"+
				"Si Windows garde une autre application, utilise « Fichier → Choisir via « Ouvrir avec »… »\n"+
				"ou clic droit sur un .md → Ouvrir avec → Toujours utiliser cette application.",
				"MdReader", MB_OK|MB_ICONINFORMATION)
		}
	case cmOpenWithDialog:
		a.openWithDialog()
	case cmRemoveAssoc:
		if err := removeAssoc(); err != nil {
			a.msgBox("Suppression impossible:\n"+err.Error(), "MdReader", MB_OK|MB_ICONERROR)
		} else {
			a.msgBox("Association retirée.", "MdReader", MB_OK|MB_ICONINFORMATION)
		}
	case cmAbout:
		a.msgBox(fmt.Sprintf("MdReader %s\n\nLecteur Markdown portable (RichEdit, sans dépendance).\nGo %s", appVersion, runtime.Version()),
			"À propos", MB_OK|MB_ICONINFORMATION)
	case cmShortcuts:
		a.msgBox("Ctrl+O\tOuvrir\nCtrl+E\tLecture / édition\nCtrl+S\tEnregistrer\nF5\tRecharger\nCtrl + molette\tZoom\nCtrl+0\tZoom 100 %\nF11\tPlein écran\nCtrl+L / Ctrl+D\tThème clair / sombre",
			"Raccourcis", MB_OK|MB_ICONINFORMATION)
	case cmFullscreen:
		a.toggleFullscreen()
	case cmEscape:
		if a.fullscreen {
			a.toggleFullscreen()
		}
	default:
		if int(id) >= cmRecentFirst && int(id) <= cmRecentLast {
			i := int(id) - cmRecentFirst
			if i < len(a.cfg.Recent) {
				a.openPath(a.cfg.Recent[i])
			}
		}
	}
}

func (a *App) reload() {
	if a.path == "" {
		return
	}
	data, err := os.ReadFile(a.path)
	if err != nil {
		a.msgBox("Rechargement impossible:\n"+err.Error(), "MdReader", MB_OK|MB_ICONERROR)
		return
	}
	if a.dirty {
		if a.msgBox("Recharger et perdre les modifications ?", "MdReader", MB_YESNOCANCEL|MB_ICONWARNING) != IDYES {
			return
		}
	}
	a.raw, a.bom, a.eol = decodeDocument(data)
	a.dirty = false
	if st, err := os.Stat(a.path); err == nil {
		a.cfg.lastMod = st.ModTime().UnixNano()
	}
	if a.inEdit {
		a.setEditorText()
	} else {
		a.renderRead()
	}
	a.updateStatus("Rechargé")
	a.setTitle()
}

func (a *App) copyPlainText() {
	text := a.mirror
	if a.inEdit {
		text = a.editorText()
	}
	if text == "" {
		return
	}
	if r, _, _ := pOpenClipboard.Call(a.hwnd); r == 0 {
		return
	}
	pEmptyClipboard.Call()
	u := utf16.Encode([]rune(strings.ReplaceAll(text, "\n", "\r\n")))
	raw := append(u, 0)
	size := uintptr(len(raw) * 2)
	h, _, _ := pGlobalAlloc.Call(0x0042, size)
	if h != 0 {
		if ptr, _, _ := kernel32.NewProc("GlobalLock").Call(h); ptr != 0 {
			dst := unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), len(raw))
			copy(dst, raw)
			kernel32.NewProc("GlobalUnlock").Call(h)
		}
		pSetClipboardData.Call(13 /*CF_UNICODETEXT*/, h)
	}
	pCloseClipboard.Call()
}

func (a *App) toggleFullscreen() {
	a.fullscreen = !a.fullscreen
	if a.fullscreen {
		pGetWindowRect.Call(a.hwnd, uintptr(unsafe.Pointer(&a.savedRect)))
		mon := user32.NewProc("MonitorFromWindow")
		m, _, _ := mon.Call(a.hwnd, 2 /*MONITOR_DEFAULTTONEAREST*/)
		var mi monitorInfo
		mi.CbSize = uint32(unsafe.Sizeof(mi))
		user32.NewProc("GetMonitorInfoW").Call(m, uintptr(unsafe.Pointer(&mi)))
		rc := mi.RcMonitor
		pSetWindowPos.Call(a.hwnd, 0,
			uintptr(rc.Left), uintptr(rc.Top),
			uintptr(rc.Right-rc.Left), uintptr(rc.Bottom-rc.Top),
			0x0040 /*SWP_SHOWWINDOW*/)
		return
	}
	pSetWindowPos.Call(a.hwnd, 0,
		uintptr(a.savedRect.Left), uintptr(a.savedRect.Top),
		uintptr(a.savedRect.Right-a.savedRect.Left), uintptr(a.savedRect.Bottom-a.savedRect.Top),
		0x0040)
}

type monitorInfo struct {
	CbSize    uint32
	RcMonitor RECT
	RcWork    RECT
	DwFlags   uint32
}

// ------------------------------------------------------------- window text

func (a *App) setTitle() {
	name := "MdReader"
	if a.path != "" {
		name = filepath.Base(a.path) + " — MdReader"
	}
	if a.dirty {
		name = "* " + name
	}
	pSetWindowTextW.Call(a.hwnd, uintptr(unsafe.Pointer(utf16Ptr(name))))
}

func (a *App) updateStatus(msg string) {
	if a.status == 0 {
		return
	}
	mode := "Lecture"
	if a.inEdit {
		mode = "Édition"
	}
	if msg == "" {
		msg = mode
	} else {
		msg = mode + " · " + msg
	}
	pSendMessageW.Call(a.status, SB_SETTEXTW, 0, uintptr(unsafe.Pointer(utf16Ptr(msg))))
	path := a.path
	if a.path == "" {
		path = "aucun fichier"
	}
	if a.dirty {
		path += "  (modifié)"
	}
	pSendMessageW.Call(a.status, SB_SETTEXTW, 1, uintptr(unsafe.Pointer(utf16Ptr(path))))
}

func (a *App) saveGeometry() {
	var rc RECT
	if ic, _, _ := pIsIconic.Call(a.hwnd); ic != 0 {
		return
	}
	pGetWindowRect.Call(a.hwnd, uintptr(unsafe.Pointer(&rc)))
	mz, _, _ := pIsZoomed.Call(a.hwnd)
	a.cfg.Maximized = mz != 0
	if !a.cfg.Maximized {
		a.cfg.X = int(rc.Left)
		a.cfg.Y = int(rc.Top)
		a.cfg.W = int(rc.Right - rc.Left)
		a.cfg.H = int(rc.Bottom - rc.Top)
	}
	a.cfg.Scale = a.scale
}

// ------------------------------------------------------------------ helpers

func (a *App) onDestroy() {
	a.saveGeometry()
}

func selftest() {
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	var b strings.Builder
	fmt.Fprintf(&b, "MdReader %s selftest\n", appVersion)
	fmt.Fprintf(&b, "sizeof(MSG)=%d (attendu 48)\n", unsafe.Sizeof(MSG{}))
	fmt.Fprintf(&b, "sizeof(WNDCLASSEXW)=%d (attendu 80)\n", unsafe.Sizeof(WNDCLASSEXW{}))
	fmt.Fprintf(&b, "sizeof(OPENFILENAMEW)=%d (attendu 152)\n", unsafe.Sizeof(OPENFILENAMEW{}))
	fmt.Fprintf(&b, "sizeof(ENLINK)=%d (attendu 48)\n", unsafe.Sizeof(enLink{}))
	fmt.Fprintf(&b, "sizeof(CHARFORMAT2W)=%d (attendu 116)\n", unsafe.Sizeof(charFormat2W{}))
	fmt.Fprintf(&b, "sizeof(EDITSTREAM)=%d (attendu 24)\n", unsafe.Sizeof(edStream{}))
	fmt.Fprintf(&b, "sizeof(ACCEL)=%d (attendu 6)\n", unsafe.Sizeof(accel{}))
	fmt.Fprintf(&b, "sizeof(GETTEXTEX)=%d (attendu 32)\n", unsafe.Sizeof(getTextEx{}))
	fmt.Fprintf(&b, "sizeof(GETTEXTLENGTHEX)=%d (attendu 8)\n", unsafe.Sizeof(getTextLengthEx{}))
	fmt.Fprintf(&b, "Msgftedit.dll=%v Riched20.dll=%v\n", loadLibrary("Msftedit.dll") != 0, loadLibrary("Riched20.dll") != 0)
	res := md.Render("# Test\n\ntexte *italique* et [lien](https://example.com)\n", md.Options{Theme: md.LightTheme()})
	fmt.Fprintf(&b, "render len=%d links=%d mirror=%q\n", len(res.RTF), len(res.Links), res.Text)
	fmt.Fprintf(&b, "systemDark=%v\n", systemDark())
	os.WriteFile(filepath.Join(dir, "mdreader-selftest.txt"), []byte(b.String()), 0o644)
}

func dumpRTF(in, out string, dark bool) {
	data, err := os.ReadFile(in)
	if err != nil {
		os.WriteFile(out, []byte("erreur: "+err.Error()), 0o644)
		return
	}
	text, _, _ := decodeDocument(data)
	theme := md.LightTheme()
	if dark {
		theme = md.DarkTheme()
	}
	res := md.Render(text, md.Options{Theme: theme, Scale: 1, WidthTwips: 9360})
	os.WriteFile(out+".rtf", []byte(res.RTF), 0o644)
	os.WriteFile(out+".txt", []byte(res.Text), 0o644)
}

func help(_ []string) {
	exe, _ := os.Executable()
	msg := "MdReader " + appVersion + "\n\n" +
		"Usage:\n  " + filepath.Base(exe) + " [fichier.md]\n\n" +
		"Options (sans interface):\n" +
		"  --dump-rtf <in.md> <prefix> [--dark]   écrit <prefix>.rtf et <prefix>.txt\n" +
		"  --selftest                             écrit mdreader-selftest.txt\n"
	os.WriteFile(filepath.Join(filepath.Dir(exe), "mdreader-help.txt"), []byte(msg), 0o644)
}

func init() {
	wndProcCb = syscall.NewCallback(wndProcTrampoline)
	streamInCb = syscall.NewCallback(streamInCB)
	_ = streamOutCB
}

func wndProcTrampoline(hwnd HWND, msg uint32, wparam WPARAM, lparam LPARAM) (res uintptr) {
	defer func() {
		if r := recover(); r != nil {
			logf("PANIC dans la fenêtre (msg=0x%04X): %v\n%s", msg, r, debug.Stack())
		}
	}()
	return app.wndProc(hwnd, msg, wparam, lparam)
}

// safeRender renders markdown without ever taking the process down.
func safeRender(src string, opt md.Options) (res md.Result, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			logf("PANIC dans md.Render: %v\n%s", r, debug.Stack())
			ok = false
		}
	}()
	res = md.Render(src, opt)
	return res, true
}

