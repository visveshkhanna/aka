package cli

import (
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var upSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}

// upIsolate leaves the apps in our console: Ctrl+C reaches each directly.
func upIsolate(*exec.Cmd) {}

// upForward has nothing to do for Ctrl+C, which the console delivered.
// For a console close each app's aka handles its own event.
func upForward(*os.Process, os.Signal) {}

var (
	upJobOnce sync.Once
	upJob     windows.Handle
)

// upTrack puts an app's aka in a job object that the kernel closes when
// aka up exits, however it exits, killing every aka in it. Each of those
// closes its own app's job in turn, so killing aka up stops everything.
// (Jobs nest since Windows 8.) Without this, a killed aka up left its apps
// running, since nothing tied their aka processes to it.
func upTrack(p *os.Process) {
	upJobOnce.Do(func() {
		job, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			return
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
			BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE},
		}
		if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
			windows.CloseHandle(job)
			return
		}
		upJob = job // held until exit; the kernel closes it then
	})
	if upJob == 0 {
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(p.Pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	windows.AssignProcessToJobObject(upJob, h)
}
