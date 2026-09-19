package kscript

import (
	"context"
	"io"
)

// Request describes one isolated run. Inputs are copied when the run starts;
// the caller must not mutate them concurrently during Run.
type Request struct {
	Task     string
	Args     []string
	Vars     map[string]any
	Env      map[string]string
	HostData map[string]any
	Dir      string
	DryRun   bool
	IO       IO
}

// IO controls process streams and bounded capture. A nil Stdin means EOF and a
// nil Stdout or Stderr means the data is discarded. CaptureLimit is a per
// stream byte bound: 0 streams without collecting, negative values are
// rejected.
type IO struct {
	Stdin        io.Reader
	Stdout       io.Writer
	Stderr       io.Writer
	CaptureLimit int64
}

// HostCall is the isolated input to a Handler. Handlers must respect ctx
// cooperatively: a Go goroutine cannot be stopped by force.
type HostCall struct {
	Name string
	Args []any
	Vars map[string]any
	Env  map[string]string
	Dir  string
}

// validate rejects a request that cannot be executed safely.
func (r Request) validate() error {
	if r.Task == "" {
		return &RunError{Kind: ErrKindInvalidRequest, Err: errf("Task is required")}
	}
	if r.IO.CaptureLimit < 0 {
		return &RunError{Kind: ErrKindInvalidRequest, Err: errf("IO.CaptureLimit must not be negative")}
	}
	if err := validateData(r.Vars, "Request.Vars"); err != nil {
		return err
	}
	if err := validateData(r.HostData, "Request.HostData"); err != nil {
		return err
	}
	for key := range r.Env {
		if key == "" {
			return &RunError{Kind: ErrKindInvalidRequest, Err: errf("Request.Env contains an empty key")}
		}
	}
	return nil
}

// newRequest copies caller data so a run never observes later mutations.
func newRequest(req Request) Request {
	out := req
	out.Args = append([]string(nil), req.Args...)
	out.Vars = cloneDataMap(req.Vars)
	out.Env = cloneStringMap(req.Env)
	out.HostData = cloneDataMap(req.HostData)
	return out
}

var _ = context.Canceled
