package gotftp

import (
	"fmt"
	"log"
	"os"
)

// LogHandler - Handle log print
type LogHandler func(string)

// SetLogHandler - set log handler to handle server's log
func SetLogHandler(handler LogHandler) {
	defaultLogHandler = handler
}

var verboseMode bool = true

// EnableVerbose - open verbose mode
func EnableVerbose(enable bool) {
	verboseMode = enable
}

var defaultLog = log.New(os.Stdout, "gotftp ", log.LstdFlags|log.Lmicroseconds)
var defaultLogHandler = func(s string) {
	defaultLog.Printf(s)
}

func logln(v ...interface{}) {
	if verboseMode {
		defaultLogHandler(fmt.Sprintln(v))
	}
}

func logf(format string, v ...interface{}) {
	if verboseMode {
		defaultLogHandler(fmt.Sprintf(format, v...))
	}
}
