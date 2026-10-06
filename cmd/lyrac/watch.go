package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

// watchProgram is `lyrac run --watch`: build and run the program, then rebuild and
// restart it each time one of the source files that build read changes, until
// interrupted. It is the restart kind of hot reload — the program starts over with
// fresh state each time; swapping code into a running program is open work in
// todo.md.
//
// **A build that fails leaves the running program alone.** Its diagnostics print and
// the old copy carries on, so a save made mid-edit costs nothing; the next good save
// replaces it. A program that exits on its own is reported, and the next change starts
// it again.
func watchProgram(o buildOptions) int {
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(interrupt)
	return watch(o, interrupt, 250*time.Millisecond)
}

// watch is watchProgram's loop, checking the files every poll and returning once
// stop delivers. The interval and the stop channel are parameters so a test can run
// it briskly and end it.
//
// **Polling, not a file-system notification API**, because the set is small (the
// program, its imports, the prelude) and a stat of each a few times a second is
// nothing next to a build, while every OS's notifications differ and editors' atomic
// saves (write a temp file, rename it over the original) defeat the naive use of
// them: the watched inode is replaced rather than written.
func watch(o buildOptions, stop <-chan os.Signal, poll time.Duration) int {
	var current *child
	defer func() {
		if current != nil {
			current.stop()
		}
	}()

	files := []string{o.path}
	seen := snapshot(files)
	rebuild := func() {
		before := snapshot(files)
		next, built := startChild(o)
		// A file read by this build is compared against its time from *before* the
		// build, so a save landing while it compiled triggers another one.
		files = append([]string{o.path}, built...)
		seen = snapshot(files)
		for f, t := range before {
			if _, ok := seen[f]; ok {
				seen[f] = t
			}
		}
		if next == nil {
			if current != nil {
				fmt.Fprintln(os.Stderr, "lyrac: build failed; the previous build keeps running")
			} else {
				fmt.Fprintln(os.Stderr, "lyrac: build failed; waiting for changes")
			}
			return
		}
		if current != nil {
			current.stop()
		}
		current = next
	}
	rebuild()

	ticker := time.NewTicker(poll)
	defer ticker.Stop()
	for {
		var exited <-chan error
		if current != nil {
			exited = current.done
		}
		select {
		case <-stop:
			return 0
		case err := <-exited:
			// Ctrl-C reaches the program too, so it may exit a moment before lyrac
			// hears of it; that is the interrupt, not an exit to report.
			select {
			case <-stop:
				return 0
			case <-time.After(poll):
			}
			status := exitStatus(current.cmd, err)
			fmt.Fprintf(os.Stderr, "lyrac: the program exited with status %d; waiting for changes\n", status)
			current.cleanUp()
			current = nil
		case <-ticker.C:
			changed := changedFile(seen, snapshot(files))
			if changed == "" {
				continue
			}
			settle(files, poll)
			fmt.Fprintf(os.Stderr, "lyrac: %s changed; rebuilding\n", changed)
			rebuild()
		}
	}
}

// child is one running build of the program: its process, the temp directory its
// executable was built in, and a channel delivering Wait's result once it exits.
type child struct {
	cmd  *exec.Cmd
	dir  string
	done chan error
}

// startChild builds o into a fresh temp directory and starts it, returning nil if the
// build failed or the program would not start. The source files the build read come
// back either way, so a failed build is still watched for its fix.
//
// Each build has a directory of its own because the previous program is still running
// from its directory while this one is built.
func startChild(o buildOptions) (*child, []string) {
	dir, err := os.MkdirTemp("", "lyrac-run-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
		return nil, nil
	}
	cmd, files, _ := prepareRun(o, dir)
	if cmd == nil {
		os.RemoveAll(dir)
		return nil, files
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: cannot run %s: %v\n", cmd.Path, err)
		os.RemoveAll(dir)
		return nil, files
	}
	c := &child{cmd: cmd, dir: dir, done: make(chan error, 1)}
	go func() { c.done <- cmd.Wait() }()
	return c, files
}

// stopGrace is how long a program has to exit after being asked before it is killed.
const stopGrace = 2 * time.Second

// stop ends the program and removes its directory. It is asked first, with SIGTERM —
// which SDL turns into a quit event, so a game closes its window as it would on the
// close button — and killed if it has not gone within stopGrace. Windows has no
// SIGTERM to send, so there it is killed at once.
func (c *child) stop() {
	if err := c.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		c.cmd.Process.Kill()
	}
	select {
	case <-c.done:
	case <-time.After(stopGrace):
		c.cmd.Process.Kill()
		<-c.done
	}
	c.cleanUp()
}

func (c *child) cleanUp() {
	os.RemoveAll(c.dir)
}

// snapshot records each file's modification time and size; a file that cannot be
// stat'ed (deleted, or mid-rename by an editor) is recorded as absent.
func snapshot(files []string) map[string]fileStamp {
	stamps := make(map[string]fileStamp, len(files))
	for _, f := range files {
		if info, err := os.Stat(f); err == nil {
			stamps[f] = fileStamp{info.ModTime(), info.Size()}
		} else {
			stamps[f] = fileStamp{}
		}
	}
	return stamps
}

type fileStamp struct {
	mod  time.Time
	size int64
}

// changedFile names a file whose stamp differs between the two snapshots, or returns
// "" when none does.
func changedFile(before, after map[string]fileStamp) string {
	for f, t := range after {
		if b, ok := before[f]; !ok || !b.mod.Equal(t.mod) || b.size != t.size {
			return f
		}
	}
	return ""
}

// settle waits until the files stop changing, briefly, so one save that writes
// several of them — or a formatter rewriting a file just after the editor wrote it —
// is one rebuild rather than two restarts. It gives up after a few polls, since a
// file being written continuously is no reason to stop rebuilding.
func settle(files []string, poll time.Duration) {
	last := snapshot(files)
	for range 4 {
		time.Sleep(poll / 2)
		now := snapshot(files)
		if changedFile(last, now) == "" {
			return
		}
		last = now
	}
}
