package runner

import (
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows has no signals to forward. Ctrl+C goes to every process attached
// to the console, so the child, which shares ours, gets it directly, as
// when run without aka. Stopping the app otherwise (takeover, a third
// Ctrl+C, leftovers) terminates its job object.
var (
	termSignal os.Signal = os.Kill
	killSignal os.Signal = os.Kill
)

var (
	jobsMu sync.Mutex
	jobs   = map[int]windows.Handle{}
)

// isolate leaves the child in our console and process group, so console
// Ctrl+C reaches it. (CREATE_NEW_PROCESS_GROUP would disable Ctrl+C for it.)
func isolate(*exec.Cmd) {}

// childGroup puts the child in a job object that kills every process in it
// when the last handle closes. Its descendants join automatically, so the
// whole tree can be stopped at once, and if aka itself dies the kernel
// closes the handle and the app goes with it. A process the child starts
// before it is assigned (right after spawn) escapes the job; reapLeftovers
// still catches it by PID and start time. The returned key names the job.
func childGroup(pid int) int {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return 0
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		windows.CloseHandle(job)
		return 0
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		windows.CloseHandle(job)
		return 0
	}
	jobsMu.Lock()
	jobs[pid] = job
	jobsMu.Unlock()
	return pid
}

// signalGroup terminates the job for a kill; other requests are left to
// the console (see above).
func signalGroup(key int, sig os.Signal) {
	if sig != os.Kill {
		return
	}
	jobsMu.Lock()
	job, ok := jobs[key]
	jobsMu.Unlock()
	if ok {
		windows.TerminateJobObject(job, 1)
	}
}

// releaseGroup closes the job, which kills anything still in it.
func releaseGroup(key int) {
	jobsMu.Lock()
	job, ok := jobs[key]
	delete(jobs, key)
	jobsMu.Unlock()
	if ok {
		windows.CloseHandle(job)
	}
}

func signalExitCode(*exec.ExitError) int { return 0 }

// notifySignals catches Ctrl+C and console close/logoff/shutdown (which Go
// reports as SIGTERM), so aka can clean up instead of dying first.
func notifySignals() chan os.Signal {
	ch := make(chan os.Signal, 4)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	return ch
}

func stopSignals(ch chan os.Signal) { signal.Stop(ch) }
