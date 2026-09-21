package taskrun

import "runtime"

const (
	isWindows = runtime.GOOS == "windows"
	isPOSIX   = runtime.GOOS != "windows"
)
