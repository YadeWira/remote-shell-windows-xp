package winapi

import (
	"syscall"
	"unsafe"
)

var (
	modkernel32 = syscall.NewLazyDLL("kernel32.dll")

	procGetVersionExW            = modkernel32.NewProc("GetVersionExW")
	procGetTickCount             = modkernel32.NewProc("GetTickCount")
	procGlobalMemoryStatusEx     = modkernel32.NewProc("GlobalMemoryStatusEx")
	procGetDiskFreeSpaceExW      = modkernel32.NewProc("GetDiskFreeSpaceExW")
	procGetLogicalDrives         = modkernel32.NewProc("GetLogicalDrives")
	procCreateToolhelp32Snapshot = modkernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW          = modkernel32.NewProc("Process32FirstW")
	procProcess32NextW           = modkernel32.NewProc("Process32NextW")
	procOpenProcess              = modkernel32.NewProc("OpenProcess")
	procTerminateProcess         = modkernel32.NewProc("TerminateProcess")
	procCloseHandle              = modkernel32.NewProc("CloseHandle")
)

const (
	th32csSnapProcess = 0x00000002
	processTerminate  = 0x0001
	maxPath           = 260
)

var invalidHandle = ^uintptr(0)

type osVersionInfoExW struct {
	OSVersionInfoSize uint32
	MajorVersion      uint32
	MinorVersion      uint32
	BuildNumber       uint32
	PlatformID        uint32
	CSDVersion        [128]uint16
	ServicePackMajor  uint16
	ServicePackMinor  uint16
	SuiteMask         uint16
	ProductType       byte
	Reserved          byte
}

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

type processEntry32W struct {
	Size            uint32
	CntUsage        uint32
	ProcessID       uint32
	DefaultHeapID   uintptr
	ModuleID        uint32
	CntThreads      uint32
	ParentProcessID uint32
	PriClassBase    int32
	Flags           uint32
	ExeFile         [maxPath]uint16
}

func OSVersion() (major, minor, build uint32, servicePack string, err error) {
	var info osVersionInfoExW
	info.OSVersionInfoSize = uint32(unsafe.Sizeof(info))
	r1, _, e1 := procGetVersionExW.Call(uintptr(unsafe.Pointer(&info)))
	if r1 == 0 {
		return 0, 0, 0, "", e1
	}
	return info.MajorVersion, info.MinorVersion, info.BuildNumber, syscall.UTF16ToString(info.CSDVersion[:]), nil
}

func UptimeSeconds() int64 {
	r1, _, _ := procGetTickCount.Call()
	return int64(uint32(r1)) / 1000
}

func MemoryStatusMB() (totalMB, freeMB int64, err error) {
	var m memoryStatusEx
	m.Length = uint32(unsafe.Sizeof(m))
	r1, _, e1 := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&m)))
	if r1 == 0 {
		return 0, 0, e1
	}
	return int64(m.TotalPhys / 1024 / 1024), int64(m.AvailPhys / 1024 / 1024), nil
}

func LogicalDriveLetters() []string {
	r1, _, _ := procGetLogicalDrives.Call()
	mask := uint32(r1)
	var drives []string
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) != 0 {
			drives = append(drives, string(rune('A'+i))+`:\`)
		}
	}
	return drives
}

func DiskFreeSpaceMB(root string) (totalMB, freeMB int64, err error) {
	p, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return 0, 0, err
	}
	var freeAvail, total, totalFree uint64
	r1, _, e1 := procGetDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&freeAvail)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r1 == 0 {
		return 0, 0, e1
	}
	return int64(total / 1024 / 1024), int64(freeAvail / 1024 / 1024), nil
}

type Process struct {
	PID  uint32
	PPID uint32
	Name string
}

func ListProcesses() ([]Process, error) {
	h, _, e1 := procCreateToolhelp32Snapshot.Call(uintptr(th32csSnapProcess), 0)
	if h == 0 || h == invalidHandle {
		return nil, e1
	}
	defer procCloseHandle.Call(h)

	var entry processEntry32W
	entry.Size = uint32(unsafe.Sizeof(entry))

	var procs []Process
	r1, _, _ := procProcess32FirstW.Call(h, uintptr(unsafe.Pointer(&entry)))
	if r1 == 0 {
		return procs, nil
	}
	for {
		procs = append(procs, Process{
			PID:  entry.ProcessID,
			PPID: entry.ParentProcessID,
			Name: syscall.UTF16ToString(entry.ExeFile[:]),
		})
		entry.Size = uint32(unsafe.Sizeof(entry))
		r1, _, _ := procProcess32NextW.Call(h, uintptr(unsafe.Pointer(&entry)))
		if r1 == 0 {
			break
		}
	}
	return procs, nil
}

func KillProcess(pid uint32) error {
	h, _, e1 := procOpenProcess.Call(uintptr(processTerminate), 0, uintptr(pid))
	if h == 0 {
		return e1
	}
	defer procCloseHandle.Call(h)
	r1, _, e2 := procTerminateProcess.Call(h, 1)
	if r1 == 0 {
		return e2
	}
	return nil
}
