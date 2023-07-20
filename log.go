package watch

import (
	"fmt"
	"os"
)

type LogType string

const (
	LogExec  LogType = "exec"
	LogSkip  LogType = "skip"
	LogWatch LogType = "watch"
	LogWrite LogType = "write"
	LogTerm  LogType = "term"
	LogStart LogType = "start"
)

func (l LogType) Color() int {
	switch l {
	case LogExec:
		return 31
	case LogSkip:
		return 37
	case LogWatch:
		return 32
	case LogWrite:
		return 33
	case LogTerm:
		return 35
	case LogStart:
		return 36
	}
	return 37
}

func Info(t LogType, format string, v ...any) {
	fmt.Fprintf(os.Stdout, "\x1b[%vm%s: %s\x1b[0m\n", t.Color(), t, fmt.Sprintf(format, v...))
}
