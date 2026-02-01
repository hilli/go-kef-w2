package cmd

import (
	"os"

	"github.com/fatih/color"
)

var (
	headerPrinter        = color.New(color.FgCyan) // .Add(color.Bold)
	contentPrinter       = color.New(color.FgYellow).Add(color.Bold)
	taskConpletedPrinter = color.New(color.FgGreen).Add(color.Bold)
	errorPrinter         = color.New(color.FgRed).Add(color.Bold)
)

// exitOnError prints an error message and exits if err is non-nil.
// The message is formatted as "<msg>: <error>".
func exitOnError(err error, msg string) {
	if err != nil {
		errorPrinter.Printf("%s: %v\n", msg, err)
		os.Exit(1)
	}
}

// exitWithError prints an error message and exits unconditionally.
func exitWithError(format string, args ...interface{}) {
	errorPrinter.Printf(format+"\n", args...)
	os.Exit(1)
}
