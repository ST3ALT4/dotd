package main

import (
	"fmt"
	"os"

	"github.com/ST3ALT4/dotd/src"
)

const usage = `dotd - keeps ~/Downloads organised

Usage:
  dotd start       start the daemon in the background
  dotd stop        stop the daemon
  dotd status      show whether it is running / starts on boot
  dotd install     start on boot (systemd user service) and start now
  dotd uninstall   remove start-on-boot
  dotd run         run in the foreground (used by systemd)
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "run":
		err = src.Run()
	case "start":
		err = src.Start()
	case "stop":
		err = src.Stop()
	case "status":
		src.Status()
	case "install":
		err = src.Install()
	case "uninstall":
		err = src.Uninstall()
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
