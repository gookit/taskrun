// Package formats decodes task definitions from YAML, JSON and TOML into the
// taskrun data model, and converts legacy Kite task maps.
//
// The package performs no execution: it never starts a process, never calls a
// handler and never evaluates a dynamic variable command. Decoding records the
// source name, base directory and field path of every error, and a definition
// is only returned after the whole document validates.
//
// Discovery of task files is always explicit: the caller supplies the start
// directory, the file names and the extension order. Nothing is read from
// parent directories or the network by default.
package formats
