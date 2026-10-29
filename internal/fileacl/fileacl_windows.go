package fileacl

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	seFileObject                     = 1
	daclSecurityInformation          = 0x4
	protectedDaclSecurityInformation = 0x80000000
	sddlRevision1                    = 1
)

var (
	advapi32                      = syscall.NewLazyDLL("advapi32.dll")
	kernel32                      = syscall.NewLazyDLL("kernel32.dll")
	procConvertSDDL               = advapi32.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW")
	procGetSecurityDescriptorDacl = advapi32.NewProc("GetSecurityDescriptorDacl")
	procSetNamedSecurityInfo      = advapi32.NewProc("SetNamedSecurityInfoW")
	procLocalFree                 = kernel32.NewProc("LocalFree")
)

// RestrictFile gives full control to SYSTEM, Administrators and the current account only, and
// stops inheriting the parent's ACL.
func RestrictFile(path string) error { return apply(path, "") }

// RestrictDir does the same for a directory, with the ACEs inherited by files and subfolders
// created in it later.
func RestrictDir(path string) error { return apply(path, "OICI") }

func apply(path, flags string) error {
	user, err := currentUserSID()
	if err != nil {
		return err
	}
	sddl := "D:P(A;" + flags + ";FA;;;SY)(A;" + flags + ";FA;;;BA)"
	if user != "S-1-5-18" { // SYSTEM is already listed
		sddl += "(A;" + flags + ";FA;;;" + user + ")"
	}

	psddl, err := syscall.UTF16PtrFromString(sddl)
	if err != nil {
		return err
	}
	var sd uintptr
	if r, _, e := procConvertSDDL.Call(uintptr(unsafe.Pointer(psddl)), sddlRevision1, uintptr(unsafe.Pointer(&sd)), 0); r == 0 {
		return fmt.Errorf("building the ACL: %v", e)
	}
	defer procLocalFree.Call(sd)

	var present, defaulted int32
	var dacl uintptr
	if r, _, e := procGetSecurityDescriptorDacl.Call(sd, uintptr(unsafe.Pointer(&present)), uintptr(unsafe.Pointer(&dacl)), uintptr(unsafe.Pointer(&defaulted))); r == 0 || present == 0 {
		return fmt.Errorf("reading the ACL: %v", e)
	}

	ppath, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	// SetNamedSecurityInfo returns the Win32 error code itself (0 = success).
	if r, _, _ := procSetNamedSecurityInfo.Call(uintptr(unsafe.Pointer(ppath)), seFileObject,
		daclSecurityInformation|protectedDaclSecurityInformation, 0, 0, dacl, 0); r != 0 {
		return fmt.Errorf("setting the ACL on %s: %v", path, syscall.Errno(r))
	}
	return nil
}

func currentUserSID() (string, error) {
	tok, err := syscall.OpenCurrentProcessToken()
	if err != nil {
		return "", err
	}
	defer tok.Close()
	u, err := tok.GetTokenUser()
	if err != nil {
		return "", err
	}
	return u.User.Sid.String()
}
