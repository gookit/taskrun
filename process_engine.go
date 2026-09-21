package taskrun

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gookit/taskrun/internal/data"
)

// ProcessEngine is the default Engine. It runs argv actions and explicit shell
// sources as child processes, resolves programs against the effective
// environment, bounds captured output and terminates the processes it owns
// when the context is canceled or a deadline expires.
type ProcessEngine struct {
	// grace is how long a canceled process tree may exit on its own before it
	// is killed. Zero falls back to DefaultKillGrace.
	grace time.Duration
	// TreeKill selects the behavior when the strongest available tree cleanup
	// cannot be established. The zero value is TreeKillAuto.
	TreeKill TreeKillMode
	// skipJobObject is used by tests to exercise the fallback controller on a
	// host where job objects work.
	skipJobObject bool
	// controlFactory replaces the platform tree controller in tests, so control
	// accounting (exactly one release per created control) can be asserted.
	controlFactory func(useJob bool) (treeControl, error)
}

// Execute implements Engine.
func (e ProcessEngine) Execute(ctx context.Context, action PreparedAction, streams IO) (ActionResult, error) {
	result := ActionResult{}
	if streams.CaptureLimit < 0 {
		return result, &ProcessError{Kind: ErrKindInvalidRequest, Err: errf("IO.CaptureLimit must not be negative")}
	}
	program, args, err := e.resolve(action)
	if err != nil {
		return result, err
	}
	// The strongest controller is created up front; when it is unavailable the
	// engine reports a start error unless TreeKillAuto allows the fallback.
	control, err := e.treeControl()
	if err != nil {
		return result, &ProcessError{Kind: ErrKindStart, Err: errf("process tree cleanup is unavailable: %v", err)}
	}
	// The deferred call reads control at exit, so the fallback path below
	// releases the owning control explicitly and this releases whichever
	// control is current. Binding the receiver here would close the owning
	// control twice.
	defer func() { control.release() }()

	cmd := exec.Command(program, args...)
	cmd.Dir = action.Dir
	if len(action.Env) > 0 {
		cmd.Env = action.Env
	} else {
		cmd.Env = os.Environ()
	}
	cmd.SysProcAttr = control.sysProcAttr()
	cmd.Stdin = streams.Stdin

	var abortOnce bool
	killNow := func() {
		if abortOnce {
			return
		}
		abortOnce = true
		control.force(cmd)
	}
	outWriter := newCaptureWriter(streams.Stdout, streams.CaptureLimit, killNow)
	errWriter := newCaptureWriter(streams.Stderr, streams.CaptureLimit, killNow)
	cmd.Stdout = outWriter
	cmd.Stderr = errWriter

	if err := cmd.Start(); err != nil {
		return result, &ProcessError{Kind: ErrKindStart, Err: err}
	}
	result.Started = true
	if err := control.attach(cmd); err != nil {
		// The strongest mechanism is unavailable, for example because a
		// restricted job already owns this process on a CI runner. TreeKillAuto
		// switches to the parent-chain controller, which still terminates the
		// whole tree; TreeKillRequired fails the action instead.
		fallback, fallbackErr := e.newControl(false)
		if e.TreeKill == TreeKillRequired || fallbackErr != nil {
			control.force(cmd)
			_ = cmd.Wait()
			return result, &ProcessError{Kind: ErrKindStart, Started: true, Err: errf("process tree cleanup is unavailable: %v", err)}
		}
		control.release()
		control = fallback
	}

	grace := e.grace
	if grace <= 0 {
		grace = DefaultKillGrace
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-done:
			return
		case <-ctx.Done():
		}
		control.graceful(cmd)
		if grace > 0 {
			timer := time.NewTimer(grace)
			defer timer.Stop()
			select {
			case <-done:
				return
			case <-timer.C:
			}
		}
		select {
		case <-done:
		default:
			control.force(cmd)
		}
	}()

	waitErr := cmd.Wait()
	close(done)

	if state := cmd.ProcessState; state != nil && result.Started {
		result.ExitCode = exitCodePtr(state.ExitCode())
	}
	result.Output = outWriter.bytes()
	result.ErrorOutput = errWriter.bytes()
	result.Truncated = outWriter.truncated || errWriter.truncated

	ctxErr := ctx.Err()
	switch {
	case ctxErr != nil:
		kind := ErrKindCanceled
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			kind = ErrKindTimedOut
		}
		return result, &ProcessError{Kind: kind, ExitCode: result.ExitCode, Started: true,
			Err: errf("process terminated after %v: %w", ctxErr, orErr(waitErr))}
	case outWriter.err != nil:
		return result, &ProcessError{Kind: ErrKindIO, ExitCode: result.ExitCode, Started: true, Err: outWriter.err}
	case errWriter.err != nil:
		return result, &ProcessError{Kind: ErrKindIO, ExitCode: result.ExitCode, Started: true, Err: errWriter.err}
	case waitErr != nil:
		return result, &ProcessError{Kind: ErrKindExit, ExitCode: result.ExitCode, Started: true, Err: waitErr}
	}
	return result, nil
}

// treeControl returns the tree controller for this engine: the strongest
// available one, or the parent-chain fallback when TreeKillAuto cannot use it.
func (e ProcessEngine) treeControl() (treeControl, error) {
	if e.skipJobObject {
		// Test hook: pretend the owning mechanism is unavailable.
		if e.TreeKill == TreeKillRequired {
			return nil, errf("process tree job object is unavailable")
		}
		return e.newControl(false)
	}
	control, err := e.newControl(true)
	if err != nil && e.TreeKill == TreeKillAuto {
		// Creating the owning job failed; the parent-chain controller still
		// terminates the tree.
		return e.newControl(false)
	}
	return control, err
}

// newControl builds a tree controller, honouring the test factory when one is
// set.
func (e ProcessEngine) newControl(useJob bool) (treeControl, error) {
	if e.controlFactory != nil {
		return e.controlFactory(useJob)
	}
	return newTreeControl(useJob)
}

func orErr(err error) error {
	if err == nil {
		return errf("no process error")
	}
	return err
}

// resolve selects the program and argv for an action.
func (e ProcessEngine) resolve(action PreparedAction) (string, []string, error) {
	env := envFromList(action.Env)
	switch action.Kind {
	case "exec", "file":
		program, err := resolveProgram(action.Program, env, action.Dir)
		if err != nil {
			return "", nil, &ProcessError{Kind: ErrKindStart, Err: err}
		}
		return program, action.Args, nil
	case "shell":
		program, args, err := shellCommand(action.Program, action.Script, action.Args, env)
		if err != nil {
			return "", nil, err
		}
		resolved, err := resolveProgram(program, env, action.Dir)
		if err != nil {
			return "", nil, &ProcessError{Kind: ErrKindStart, Err: err}
		}
		return resolved, args, nil
	default:
		return "", nil, &ProcessError{Kind: ErrKindStart, Err: errf("unknown action kind %q", action.Kind)}
	}
}

// shellCommand maps an explicit shell name to its interpreter invocation. Each
// shell keeps its own argument contract; scripts are never guessed from a
// shebang or an extension.
func shellCommand(name, script string, prefixArgs []string, env map[string]string) (string, []string, error) {
	normalized := normalizeShellName(name)
	switch normalized {
	case "sh", "bash", "zsh":
		return normalized, append(data.CloneStrings(prefixArgs), "-c", script), nil
	case "cmd":
		program := "cmd.exe"
		if comspec, ok := lookupEnv(env, "ComSpec"); ok && comspec != "" {
			program = comspec
		}
		return program, append(data.CloneStrings(prefixArgs), "/D", "/S", "/C", script), nil
	case "pwsh", "powershell":
		return normalized, append(data.CloneStrings(prefixArgs), "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script), nil
	default:
		return "", nil, &ProcessError{Kind: ErrKindStart, Err: errf("unsupported shell %q; supported: sh, bash, zsh, cmd, pwsh, powershell", name)}
	}
}

// resolveProgram finds an executable using the effective PATH instead of the
// process PATH, so a step or task level PATH change takes effect. When the
// effective environment has no PATH at all, the process PATH is used.
func resolveProgram(program string, env map[string]string, dir string) (string, error) {
	if strings.TrimSpace(program) == "" {
		return "", errf("empty program name")
	}
	if filepath.IsAbs(program) {
		if isExecutableFile(program) {
			return program, nil
		}
		return "", errf("program %q is not an executable file", program)
	}
	if strings.ContainsAny(program, `/\`) {
		candidate := program
		if dir != "" {
			candidate = filepath.Join(dir, program)
		}
		if isExecutableFile(candidate) {
			return candidate, nil
		}
		return "", errf("program %q is not an executable file", candidate)
	}
	pathValue, ok := lookupEnv(env, "PATH")
	if !ok || pathValue == "" {
		pathValue = os.Getenv("PATH")
	}
	extensions := []string{""}
	if data.IsWindows {
		extensions = windowsExecutableExtensions(env)
	}
	for _, entry := range filepath.SplitList(pathValue) {
		entry = strings.Trim(strings.TrimSpace(entry), `"`)
		if entry == "" {
			continue
		}
		for _, extension := range extensions {
			candidate := filepath.Join(entry, program+extension)
			if isExecutableFile(candidate) {
				return candidate, nil
			}
		}
	}
	return "", errf("program %q was not found in PATH", program)
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if data.IsWindows {
		return true
	}
	return info.Mode().Perm()&0o111 != 0
}

func windowsExecutableExtensions(env map[string]string) []string {
	value, ok := lookupEnv(env, "PATHEXT")
	if !ok || value == "" {
		value = os.Getenv("PATHEXT")
	}
	if value == "" {
		value = ".COM;.EXE;.BAT;.CMD"
	}
	parts := strings.Split(value, ";")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, strings.ToLower(part))
	}
	return out
}

// captureWriter forwards every byte to an optional destination, keeps a bounded
// copy for the result and keeps draining after the bound is reached so the
// child never blocks on a full pipe.
type captureWriter struct {
	dest      io.Writer
	limit     int64
	abort     func()
	collected []byte
	err       error
	truncated bool
}

func newCaptureWriter(dest io.Writer, limit int64, abort func()) *captureWriter {
	return &captureWriter{dest: dest, limit: limit, abort: abort}
}

func (w *captureWriter) Write(p []byte) (int, error) {
	if w.dest != nil {
		if _, err := w.dest.Write(p); err != nil {
			w.err = err
			if w.abort != nil {
				w.abort()
			}
			return 0, err
		}
	}
	if w.limit > 0 {
		remaining := w.limit - int64(len(w.collected))
		switch {
		case remaining <= 0:
			w.truncated = true
		case int64(len(p)) > remaining:
			w.collected = append(w.collected, p[:remaining]...)
			w.truncated = true
		default:
			w.collected = append(w.collected, p...)
		}
	}
	return len(p), nil
}

func (w *captureWriter) bytes() []byte {
	if w.limit <= 0 || len(w.collected) == 0 {
		return nil
	}
	return w.collected
}
