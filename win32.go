//go:build windows

package main

// Minimal Win32 bindings: everything the reader needs, no external deps.

import (
	"syscall"
	"unsafe"
)

type (
	HWND      = uintptr
	HINSTANCE = uintptr
	HMENU     = uintptr
	HICON     = uintptr
	HCURSOR   = uintptr

	WPARAM  = uintptr
	LPARAM  = uintptr
	LRESULT = uintptr
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	comdlg32 = syscall.NewLazyDLL("comdlg32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	comctl32 = syscall.NewLazyDLL("comctl32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")
)

var (
	pRegisterClassExW           = user32.NewProc("RegisterClassExW")
	pCreateWindowExW            = user32.NewProc("CreateWindowExW")
	pDefWindowProcW             = user32.NewProc("DefWindowProcW")
	pDestroyWindow              = user32.NewProc("DestroyWindow")
	pShowWindow                 = user32.NewProc("ShowWindow")
	pUpdateWindow               = user32.NewProc("UpdateWindow")
	pGetMessageW                = user32.NewProc("GetMessageW")
	pTranslateMessage           = user32.NewProc("TranslateMessage")
	pDispatchMessageW           = user32.NewProc("DispatchMessageW")
	pPostQuitMessage            = user32.NewProc("PostQuitMessage")
	pTranslateAcceleratorW      = user32.NewProc("TranslateAcceleratorW")
	pCreateAcceleratorTableW    = user32.NewProc("CreateAcceleratorTableW")
	pLoadCursorW                = user32.NewProc("LoadCursorW")
	pLoadIconW                  = user32.NewProc("LoadIconW")
	pSendMessageW               = user32.NewProc("SendMessageW")
	pPostMessageW               = user32.NewProc("PostMessageW")
	pSetWindowTextW             = user32.NewProc("SetWindowTextW")
	pMessageBoxW                = user32.NewProc("MessageBoxW")
	pGetClientRect              = user32.NewProc("GetClientRect")
	pGetWindowRect              = user32.NewProc("GetWindowRect")
	pMoveWindow                 = user32.NewProc("MoveWindow")
	pSetWindowPos               = user32.NewProc("SetWindowPos")
	pSetTimer                   = user32.NewProc("SetTimer")
	pKillTimer                  = user32.NewProc("KillTimer")
	pSetFocus                   = user32.NewProc("SetFocus")
	pCreatePopupMenu            = user32.NewProc("CreatePopupMenu")
	pCreateMenu                 = user32.NewProc("CreateMenu")
	pAppendMenuW                = user32.NewProc("AppendMenuW")
	pSetMenu                   = user32.NewProc("SetMenu")
	pDestroyMenu                = user32.NewProc("DestroyMenu")
	pTrackPopupMenu             = user32.NewProc("TrackPopupMenu")
	pCheckMenuRadioItem         = user32.NewProc("CheckMenuRadioItem")
	pCheckMenuItem              = user32.NewProc("CheckMenuItem")
	pEnableMenuItem             = user32.NewProc("EnableMenuItem")
	pGetCursorPos               = user32.NewProc("GetCursorPos")
	pSetForegroundWindow        = user32.NewProc("SetForegroundWindow")
	pIsZoomed                   = user32.NewProc("IsZoomed")
	pIsIconic                   = user32.NewProc("IsIconic")
	pGetSystemMetrics           = user32.NewProc("GetSystemMetrics")
	pSetProcessDPIAware         = user32.NewProc("SetProcessDPIAware")
	pSetProcessDpiAwarenessCtx  = user32.NewProc("SetProcessDpiAwarenessContext")
	pGetDpiForWindow            = user32.NewProc("GetDpiForWindow")
	pOpenClipboard              = user32.NewProc("OpenClipboard")
	pEmptyClipboard             = user32.NewProc("EmptyClipboard")
	pSetClipboardData           = user32.NewProc("SetClipboardData")
	pCloseClipboard             = user32.NewProc("CloseClipboard")
	pRegisterClipboardFormatW   = user32.NewProc("RegisterClipboardFormatW")
	pDragAcceptFiles            = shell32.NewProc("DragAcceptFiles")
	pDragQueryFileW             = shell32.NewProc("DragQueryFileW")
	pDragFinish                 = shell32.NewProc("DragFinish")
	pShellExecuteW              = shell32.NewProc("ShellExecuteW")
	pSHChangeNotify             = shell32.NewProc("SHChangeNotify")
	pGetOpenFileNameW           = comdlg32.NewProc("GetOpenFileNameW")
	pGetModuleFileNameW         = kernel32.NewProc("GetModuleFileNameW")
	pLoadLibraryW               = kernel32.NewProc("LoadLibraryW")
	pGlobalAlloc                = kernel32.NewProc("GlobalAlloc")
	pGlobalFree                 = kernel32.NewProc("GlobalFree")
	pRegCreateKeyExW            = advapi32.NewProc("RegCreateKeyExW")
	pRegSetValueExW             = advapi32.NewProc("RegSetValueExW")
	pRegQueryValueExW           = advapi32.NewProc("RegQueryValueExW")
	pRegDeleteTreeW             = advapi32.NewProc("RegDeleteTreeW")
	pRegDeleteValueW            = advapi32.NewProc("RegDeleteValueW")
	pRegCloseKey                = advapi32.NewProc("RegCloseKey")
	pInitCommonControlsEx       = comctl32.NewProc("InitCommonControlsEx")
	pCoInitializeEx             = ole32.NewProc("CoInitializeEx")
)

// ------------------------------------------------------------------ structs

type POINT struct{ X, Y int32 }

type RECT struct{ Left, Top, Right, Bottom int32 }

type MSG struct {
	Hwnd    HWND
	Message uint32
	WParam  WPARAM
	LParam  LPARAM
	Time    uint32
	Pt      POINT
}

type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     HINSTANCE
	HIcon         HICON
	HCursor       HCURSOR
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       HICON
}

type OPENFILENAMEW struct {
	StructSize    uint32
	Owner         HWND
	Instance      HINSTANCE
	Filter        *uint16
	CustomFilter  *uint16
	MaxCustFilter uint32
	FilterIndex   uint32
	File          *uint16
	MaxFile       uint32
	FileTitle     *uint16
	MaxFileTitle  uint32
	InitialDir    *uint16
	Title         *uint16
	Flags         uint32
	FileOffset    uint16
	FileExtension uint16
	DefExt        *uint16
	CustData      uintptr
	Hook          uintptr
	TemplateName  *uint16
	PvReserved    uintptr
	DwReserved    uint32
	FlagsEx       uint32
}

type edStream struct {
	Cookie    uintptr
	Error     uint32
	PfnCallback uintptr
}

type charRange struct{ CpMin, CpMax int32 }

type charFormat2W struct {
	CbSize          uint32
	DwMask          uint32
	DwEffects       uint32
	YHeight         int32
	YOffset         int32
	CrTextColor     uint32
	BCharSet        byte
	BPitchAndFamily byte
	SzFaceName      [32]uint16
	WWeight         uint16
	SSpacing        int16
	CrBackColor     uint32
	Lcid            uint32
	DwReserved      uint32
	SStyle          int16
	WKerning        uint16
	BUnderlineType  byte
	BAnimation      byte
	BRevAuthor      byte
	BReserved1      byte
}

type getTextEx struct {
	Cb           uint32
	Flags        uint32
	Codepage     uint32
	LpDefaultChr *byte
	LpUsedDefChr *byte
}

type getTextLengthEx struct {
	Flags    uint32
	Codepage uint32
}

type accel struct {
	FVirt byte
	Key   uint16
	Cmd   uint16
}

type initCommonControlsEx struct {
	Size uint32
	ICC  uint32
}

// enLink mirrors the Win64 ENLINK notification layout exactly (NMHDR is 16
// bytes on the wire, so no Go-style padding may be inserted).
type enLink struct {
	HwndFrom HWND
	IdFrom   uintptr
	Code     uint32
	Msg      uint32
	WParam   WPARAM
	LParam   LPARAM
	ChrgMin  int32
	ChrgMax  int32
}

// ------------------------------------------------------------------ consts

const (
	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_VISIBLE          = 0x10000000
	WS_CHILD            = 0x40000000
	WS_VSCROLL          = 0x00200000
	WS_TABSTOP          = 0x00010000
	WS_CLIPCHILDREN     = 0x02000000
	WS_EX_ACCEPTFILES   = 0x00000010
	WS_EX_CLIENTEDGE    = 0x00000200
	CW_USEDEFAULT       = 0x80000000

	ES_MULTILINE  = 0x0004
	ES_NOHIDESEL  = 0x0100
	ES_AUTOVSCROLL = 0x0040

	WM_CREATE          = 0x0001
	WM_DESTROY         = 0x0002
	WM_SIZE            = 0x0005
	WM_SETFOCUS        = 0x0007
	WM_CLOSE           = 0x0010
	WM_SETTINGCHANGE   = 0x001A
	WM_NOTIFY          = 0x004E
	WM_COMMAND         = 0x0111
	WM_TIMER           = 0x0113
	WM_INITMENUPOPUP   = 0x0117
	WM_MOUSEWHEEL      = 0x020A
	WM_DROPFILES       = 0x0233
	WM_DPICHANGED      = 0x02E0
	WM_LBUTTONUP       = 0x0202

	EN_CHANGE = 0x0300
	EN_LINK   = 0x0700
	ENM_LINK  = 0x04000000

	EM_EXSETSEL      = 0x0437
	EM_SETREADONLY   = 0x041F
	EM_HIDESELECTION = 0x043F
	EM_SETBKGNDCOLOR = 0x0443
	EM_SETCHARFORMAT = 0x0444
	EM_SETEVENTMASK  = 0x0445
	EM_STREAMIN      = 0x0449
	EM_EXLIMITTEXT   = 0x0435
	EM_SETUNDOLIMIT  = 0x0452
	EM_GETTEXTEX     = 0x045E
	EM_GETTEXTLENGTHEX = 0x045F
	EM_SETTEXTEX       = 0x0461
	EM_SETOPTIONS      = 0x044D

	SF_RTF = 0x0002

	SCF_ALL       = 0x0004
	SCF_SELECTION = 0x0001

	CFM_BOLD       = 0x00000001
	CFM_ITALIC     = 0x00000002
	CFM_UNDERLINE  = 0x00000004
	CFM_STRIKEOUT  = 0x00000008
	CFM_LINK       = 0x00000020
	CFM_FACE       = 0x20000000
	CFM_SIZE       = 0x80000000
	CFM_COLOR      = 0x40000000
	CFM_BACKCOLOR  = 0x04000000
	CFE_LINK       = 0x00000020
	CFE_AUTOBACKCOLOR = 0x04000000

	GTL_NUMCHARS = 0x00000008
	GTL_PRECISE  = 0x00000002
	GT_USECRLF   = 0x00000001

	SW_SHOW = 5
	SW_RESTORE = 9

	MB_OK              = 0x00000000
	MB_YESNOCANCEL     = 0x00000003
	MB_ICONERROR       = 0x00000010
	MB_ICONINFORMATION = 0x00000040
	MB_ICONQUESTION    = 0x00000020
	MB_ICONWARNING     = 0x00000030
	IDYES              = 6
	IDNO               = 7
	IDCANCEL           = 2
	IDOK               = 1

	MF_STRING    = 0x00000000
	MF_POPUP     = 0x00000010
	MF_SEPARATOR = 0x00000800
	MF_CHECKED   = 0x00000008
	MF_GRAYED    = 0x00000001
	MF_DISABLED  = 0x00000002
	MF_BYCOMMAND = 0x00000000
	MF_BYPOSITION = 0x00000400
	TPM_RIGHTBUTTON = 0x0002

	OFN_FILEMUSTEXIST = 0x00001000
	OFN_PATHMUSTEXIST = 0x00000800
	OFN_HIDEREADONLY  = 0x00000004
	OFN_EXPLORER      = 0x00080000

	WM_USER          = 0x0400
	SB_SETPARTS      = WM_USER + 4
	SB_SETTEXTW      = WM_USER + 11
	SBT_NOBORDERS    = 0x0100
	SBT_POPOUT       = 0x0200

	ICC_BAR_CLASSES = 0x00000004

	FVIRTKEY  = 0x01
	FCONTROL  = 0x08
	FSHIFT    = 0x04

	VK_F5 = 0x74
	VK_F11 = 0x7A

	SHCNE_ASSOCCHANGED = 0x08000000
	SHCNF_IDLIST       = 0x0000

	GMT_INTERNAL = 0xFFFFFFFF

	KEY_READ       = 0x20019
	KEY_WRITE      = 0x20006
	KEY_ALL_ACCESS = 0xF003F
	REG_SZ         = 1
	REG_DWORD      = 4
	HKEY_CURRENT_USER = 0x80000001
)

// ------------------------------------------------------------------ helpers

func utf16Ptr(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		p, _ = syscall.UTF16PtrFromString("")
	}
	return p
}

func loWord(v uintptr) uint16   { return uint16(v & 0xFFFF) }
func hiWord(v uintptr) uint16   { return uint16((v >> 16) & 0xFFFF) }
func loWord32(v uintptr) uint32 { return uint32(v & 0xFFFFFFFF) }

func makeIntResource(i uint32) *uint16 { return (*uint16)(unsafe.Pointer(uintptr(i))) }

func loword(v int32) int32 { return int32(uint16(v)) }
