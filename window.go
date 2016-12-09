package main

import (
	"github.com/cwchiu/go-winapi"
	"os"
	"sync"
	"unsafe"
	"syscall"
	"errors"
)

const (
	WINDOW_CLASS = "dacko"
)

const (
	MOD_ALT = 1
	MOD_CONTROL = 2
	MOD_SHIFT = 4
	MOD_WIN = 8
)

const (
	SYNCHRONIZE = 0x00100000
)

const (
	CREATE_NEW_CONSOLE = 0x00000010
)

const (
	WAIT_OBJECT_0 = 0x00000000
	WAIT_ABANDONED = 0x00000080
	WAIT_timeout = 0x00000102
	WAIT_FAILED = 0xFFFFFFFF
)

var (
	user32, _         = syscall.LoadLibrary("user32.dll")
	registerHotkey, _ = syscall.GetProcAddress(user32, "RegisterHotKey")
	getWindowThreadProcessId, _ = syscall.GetProcAddress(user32, "GetWindowThreadProcessId")
	enumWindows, _ = syscall.GetProcAddress(user32, "EnumWindows")
	procReplyMessage = syscall.NewLazyDLL("user32.dll").NewProc("ReplyMessage")

	kernel32, _            = syscall.LoadLibrary("kernel32.dll")
	waitForSingleObject, _ = syscall.GetProcAddress(kernel32, "WaitForSingleObject")
	dwProcessId uint32 = 0
	zeroProcAttr syscall.ProcAttr
	zeroSysProcAttr syscall.SysProcAttr
	ForkLock sync.RWMutex
)

func RegisterHotKey(hWnd winapi.HWND, id int, fsModifiers, vk uint) bool {
	ret, _, _ := syscall.Syscall6(uintptr(registerHotkey), 4,
		uintptr(hWnd),
		uintptr(id),
		uintptr(fsModifiers),
		uintptr(vk),0,0)

	return ret != 0
}

func GetWindowThreadProcessId(hWnd syscall.Handle) (winapi.HANDLE, uint32) {
	var id int
	ret, _, _ := syscall.Syscall(uintptr(getWindowThreadProcessId), 2,
		uintptr(hWnd),
		uintptr(unsafe.Pointer(&id)),
		0)

	return winapi.HANDLE(ret), uint32(id)
}

func EnumWindows(f func(syscall.Handle, uintptr) uintptr, lParam uint32) bool {
	ret, _, _ := syscall.Syscall(uintptr(enumWindows), 2,
		uintptr(syscall.NewCallback(f)),
		uintptr(lParam),
		0)
	
	return ret != 0
}

func createProcess(attr *syscall.ProcAttr) (pid int, handle uintptr, err error) {
	if attr == nil {
		attr = &zeroProcAttr
	}
	sys := attr.Sys
	if sys == nil {
		sys = &zeroSysProcAttr
	}

	if len(attr.Files) > 3 {
		return 0, 0, syscall.EWINDOWS
	}
	if len(attr.Files) < 3 {
		return 0, 0, syscall.EINVAL
	}

	var cmdline string
	// Windows CreateProcess takes the command line as a single string:
	// use attr.CmdLine if set, else build the command line by escaping
	// and joining each argument with spaces
	if sys.CmdLine != "" {
		cmdline = sys.CmdLine
	} else {
		return 0, 0, errors.New("sys.CmdLine is empty")
	}

	var argvp *uint16
	if len(cmdline) != 0 {
		argvp, err = syscall.UTF16PtrFromString(cmdline)
		if err != nil {
			return 0, 0, err
		}
	}

	var dirp *uint16
	if len(attr.Dir) != 0 {
		dirp, err = syscall.UTF16PtrFromString(attr.Dir)
		if err != nil {
			return 0, 0, err
		}
	}

	// Acquire the fork lock so that no other threads
	// create new fds that are not yet close-on-exec
	// before we fork.
	ForkLock.Lock()
	defer ForkLock.Unlock()

	p, _ := syscall.GetCurrentProcess()
	fd := make([]syscall.Handle, len(attr.Files))
	for i := range attr.Files {
		if attr.Files[i] > 0 {
			err := syscall.DuplicateHandle(p, syscall.Handle(attr.Files[i]), p, &fd[i], 0, true, syscall.DUPLICATE_SAME_ACCESS)
			if err != nil {
				return 0, 0, err
			}
			defer syscall.CloseHandle(syscall.Handle(fd[i]))
		}
	}
	si := new(syscall.StartupInfo)
	si.Cb = uint32(unsafe.Sizeof(*si))

	pi := new(syscall.ProcessInformation)

	flags := sys.CreationFlags | syscall.CREATE_UNICODE_ENVIRONMENT
	err = syscall.CreateProcess(nil, argvp, nil, nil, false, flags, nil, dirp, si, pi)
	if err != nil {
		return 0, 0, err
	}
	defer syscall.CloseHandle(syscall.Handle(pi.Thread))

	return int(pi.ProcessId), uintptr(pi.Process), nil
}

func showErrorMessage(hWnd winapi.HWND, msg string) {
	s, _ := syscall.UTF16PtrFromString(msg)
	t, _ := syscall.UTF16PtrFromString(WINDOW_CLASS)
	winapi.MessageBox(hWnd, s, t, winapi.MB_ICONWARNING|winapi.MB_OK)
}


func run() int {
	hInstance := winapi.GetModuleHandle(nil)

	if registerWindowClass(hInstance) == 0 {
		showErrorMessage(0, "registerWindowClass failed")
		return 1
	}

	if err := initializeInstance(hInstance, winapi.SW_SHOW); err != nil {
		showErrorMessage(0, err.Error())
		return 1
	}

	var msg winapi.MSG
	for winapi.GetMessage(&msg, 0,0,0) != 0 {
		winapi.TranslateMessage(&msg)
		winapi.DispatchMessage(&msg)
	}

	finalizeInstance(hInstance)

	return int(msg.WParam)
}

func registerWindowClass(hInstance winapi.HINSTANCE) winapi.ATOM {
	var wc winapi.WNDCLASSEX

	wc.CbSize = uint32(unsafe.Sizeof(winapi.WNDCLASSEX{}))
	wc.Style = 0
	wc.LpfnWndProc = syscall.NewCallback(wndProc)
	wc.CbClsExtra = 0
	wc.CbWndExtra = 0
	wc.HInstance = hInstance
	wc.HIcon         = winapi.LoadIcon(hInstance, winapi.MAKEINTRESOURCE(132))
	wc.HCursor       = winapi.LoadCursor(0, winapi.MAKEINTRESOURCE(winapi.IDC_HAND))
	wc.HbrBackground = winapi.HBRUSH(winapi.GetStockObject(winapi.WHITE_BRUSH))
	wc.LpszMenuName  = nil
	wc.LpszClassName,_ = syscall.UTF16PtrFromString(WINDOW_CLASS)

	return winapi.RegisterClassEx(&wc)
}

func initializeInstance(hInstance winapi.HINSTANCE, nCmdShow int) error {
	pc, _ := syscall.UTF16PtrFromString(WINDOW_CLASS)
	pt, _ := syscall.UTF16PtrFromString(WINDOW_CLASS)

	hWnd := winapi.CreateWindowEx(
		winapi.WS_EX_TOOLWINDOW|winapi.WS_EX_TOPMOST|winapi.WS_EX_NOACTIVATE|winapi.WS_EX_LAYERED,
		pc,pt,winapi.WS_POPUP,
		0,
		0,
		0,
		0,
		0,0,hInstance,nil)

	if hWnd == 0 {
		return errors.New("CreateWindowEx failed")
	}

	winapi.ShowWindow(hWnd, int32(nCmdShow))
	winapi.UpdateWindow(hWnd)

	if (RegisterHotKey(hWnd, 0, MOD_ALT|MOD_CONTROL, uint(config.Key)) == false) {
		return errors.New("RegisterHotKey failed")
	}

	return nil
}

func finalizeInstance(hInstance winapi.HINSTANCE) error {
	return nil
}

func isProcessGone(id uint32) bool {
	hProcess,err := syscall.OpenProcess(SYNCHRONIZE, false, id);
	if err != nil {
		return true;
	}

	dwWait,err := syscall.WaitForSingleObject(hProcess,0);
	if err != nil {
		return false
	}

	err = syscall.CloseHandle(hProcess)
	if err != nil {
		return false
	}

	return(dwWait == WAIT_OBJECT_0);
}

func createChildProcess(id uint32) (uint32,error) {
	if(!isProcessGone(id)) {
		return id,nil;
	}

	var sys syscall.SysProcAttr
	var attr syscall.ProcAttr

	sys.CreationFlags = CREATE_NEW_CONSOLE
	sys.CmdLine       = config.Cmdline

	attr.Files = make([]uintptr, 3)
	attr.Sys = &sys
	if config.WorkDir == "" {
		attr.Dir = os.Getenv("USERPROFILE")
	} else {
		attr.Dir = config.WorkDir
	}

	pid, _, err := createProcess(&attr)
	if err != nil {
		return 0,err
	}

	return uint32(pid), nil
}

func toggleWindowbyProcID(hwnd syscall.Handle, lparam uintptr) uintptr {
	var pid uint32
	_, pid = GetWindowThreadProcessId(hwnd)

	if (uint32(lparam) == pid) {
		if (winapi.IsWindowVisible(winapi.HWND(hwnd))) {
			winapi.ShowWindow(winapi.HWND(hwnd), winapi.SW_MINIMIZE);
			winapi.ShowWindow(winapi.HWND(hwnd), winapi.SW_HIDE);
		} else {
			winapi.ShowWindow(winapi.HWND(hwnd), winapi.SW_SHOW);
			winapi.ShowWindow(winapi.HWND(hwnd), winapi.SW_RESTORE);
		}
		return 0
	}
	return 1
}

func wndProc(hWnd winapi.HWND, msg uint32, wParam uintptr, lParam uintptr) uintptr {
	switch msg {
	case winapi.WM_HOTKEY:
		var err error
		dwProcessId,err = createChildProcess(dwProcessId);
		if err != nil {
			procReplyMessage.Call(1)
			return 0
		}

		EnumWindows(toggleWindowbyProcID, dwProcessId)
		return 0
	case winapi.WM_DESTROY:
		winapi.PostQuitMessage(0)
	default:
		return winapi.DefWindowProc(hWnd, msg, wParam, lParam)
	}
	return 0
}
