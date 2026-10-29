package session

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"unsafe"

	"github.com/BaseMax/remote-shell-windows-xp/internal/protocol"
)

const (
	createSuspended   = 0x00000004
	createNewConsole  = 0x00000010
	processSetQuota   = 0x0100
	processTerminate  = 0x0001
	processSuspendRes = 0x0800
	startfUseShowWin  = 0x00000001
	swShowNormal      = 1
	infinite          = 0xFFFFFFFF
	desktopName       = `WinSta0\Default`
)

var (
	modKernel32      = syscall.NewLazyDLL("kernel32.dll")
	modNtdll         = syscall.NewLazyDLL("ntdll.dll")
	procCreateJob    = modKernel32.NewProc("CreateJobObjectW")
	procAssignJob    = modKernel32.NewProc("AssignProcessToJobObject")
	procTermJob      = modKernel32.NewProc("TerminateJobObject")
	procResumeThrd   = modKernel32.NewProc("ResumeThread")
	procNtResumeProc = modNtdll.NewProc("NtResumeProcess")
)

// procGroup wraps a Job Object holding a command and every process it starts, so that a kill
// reaches the whole tree, including children that detached with "start" (taskkill /T misses those).
type procGroup struct {
	job syscall.Handle
}

func newJob() syscall.Handle {
	h, _, _ := procCreateJob.Call(0, 0)
	return syscall.Handle(h)
}

func assignJob(job, process syscall.Handle) bool {
	r, _, _ := procAssignJob.Call(uintptr(job), uintptr(process))
	return r != 0
}

// prepareGroup makes the shell start suspended, so it can be put in the job before it runs
// (otherwise a fast command could start children that escape the job).
func prepareGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr != nil {
		cmd.SysProcAttr.CreationFlags |= createSuspended
	}
}

// attachGroup puts the (suspended) process in a new job and resumes it. It always resumes,
// even if the job could not be set up; the group is then nil-safe and falls back to taskkill.
func attachGroup(p *os.Process) *procGroup {
	g := &procGroup{}
	h, err := syscall.OpenProcess(processSetQuota|processTerminate|processSuspendRes, false, uint32(p.Pid))
	if err != nil {
		resumeByPid(p.Pid)
		return g
	}
	defer syscall.CloseHandle(h)
	if job := newJob(); job != 0 {
		if assignJob(job, h) {
			g.job = job
		} else {
			syscall.CloseHandle(job)
		}
	}
	procNtResumeProc.Call(uintptr(h))
	return g
}

func resumeByPid(pid int) {
	h, err := syscall.OpenProcess(processSuspendRes, false, uint32(pid))
	if err != nil {
		return
	}
	procNtResumeProc.Call(uintptr(h))
	syscall.CloseHandle(h)
}

func (g *procGroup) kill(p *os.Process) {
	if g != nil && g.job != 0 {
		procTermJob.Call(uintptr(g.job), 1)
		return
	}
	exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(p.Pid)).Run()
	p.Kill()
}

// release drops the handle once the command is done. Without the kill-on-close limit this does
// not touch processes that were left running on purpose.
func (g *procGroup) release() {
	if g != nil && g.job != 0 {
		syscall.CloseHandle(g.job)
		g.job = 0
	}
}

// runDesktop starts the command on the interactive desktop (WinSta0\Default) so its windows are
// visible to whoever is logged in on the console. The agent usually runs as SYSTEM from a task,
// whose own window station is not the visible one. Output is not captured. Without req.Wait the
// result is sent at once with the process id; with it, the exit code is reported when the process ends.
func (m *Manager) runDesktop(streamID uint32, cwd string, req protocol.ExecStartRequest, onStdout func([]byte)) (io.WriteCloser, chan<- struct{}, <-chan Result) {
	killCh := make(chan struct{})
	resultCh := make(chan Result, 1)
	fail := func(err error) (io.WriteCloser, chan<- struct{}, <-chan Result) {
		resultCh <- Result{ExitCode: -1, Cwd: cwd, Err: err}
		return nil, killCh, resultCh
	}

	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\WINDOWS`
	}
	app, err := syscall.UTF16PtrFromString(root + `\system32\cmd.exe`)
	if err != nil {
		return fail(err)
	}
	line, err := syscall.UTF16PtrFromString(`cmd.exe /S /C "` + req.Command + `"`)
	if err != nil {
		return fail(err)
	}
	dir, err := syscall.UTF16PtrFromString(cwd)
	if err != nil {
		return fail(err)
	}
	desk, _ := syscall.UTF16PtrFromString(desktopName)

	si := &syscall.StartupInfo{Desktop: desk, Flags: startfUseShowWin, ShowWindow: swShowNormal}
	si.Cb = uint32(unsafe.Sizeof(*si))
	var pi syscall.ProcessInformation
	if err := syscall.CreateProcess(app, line, nil, nil, false, createSuspended|createNewConsole, nil, dir, si, &pi); err != nil {
		return fail(fmt.Errorf("cannot start on the desktop: %v", err))
	}

	group := &procGroup{}
	if job := newJob(); job != 0 {
		if assignJob(job, pi.Process) {
			group.job = job
		} else {
			syscall.CloseHandle(job)
		}
	}
	procResumeThrd.Call(uintptr(pi.Thread))
	syscall.CloseHandle(pi.Thread)

	if !req.Wait {
		onStdout([]byte("started pid " + strconv.Itoa(int(pi.ProcessId)) + " on " + desktopName + newline))
		group.release()
		syscall.CloseHandle(pi.Process)
		resultCh <- Result{ExitCode: 0, Cwd: cwd}
		return nil, killCh, resultCh
	}

	done := make(chan struct{})
	go func() {
		syscall.WaitForSingleObject(pi.Process, infinite)
		close(done)
	}()
	go func() {
		defer syscall.CloseHandle(pi.Process)
		defer group.release()
		select {
		case <-killCh:
			if group.job != 0 {
				procTermJob.Call(uintptr(group.job), 1)
			} else {
				syscall.TerminateProcess(pi.Process, 1)
			}
			<-done
			resultCh <- Result{ExitCode: -1, Cwd: cwd, Err: fmt.Errorf("killed")}
		case <-done:
			var code uint32
			syscall.GetExitCodeProcess(pi.Process, &code)
			resultCh <- Result{ExitCode: int(code), Cwd: cwd}
		}
	}()
	return nil, killCh, resultCh
}
