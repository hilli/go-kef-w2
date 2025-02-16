package cmd

import (
	"github.com/fatih/color"
)

var (
	headerPrinter        = color.New(color.FgCyan) // .Add(color.Bold)
	contentPrinter       = color.New(color.FgYellow).Add(color.Bold)
	taskConpletedPrinter = color.New(color.FgGreen).Add(color.Bold)
	errorPrinter         = color.New(color.FgRed).Add(color.Bold)
)
