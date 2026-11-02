// Command mins is a self-contained media minimizer: AV1 video, WebP/AVIF/JXL
// images, animated images, and bitrate-gated audio, with the whole toolchain
// bundled.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"mins/internal/app"
	"mins/internal/cli"
	"mins/internal/config"
	"mins/internal/core"
	"mins/internal/logx"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(argv []string) int {
	o, err := cli.Parse(argv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mins: error: "+err.Error())
		return 2
	}
	switch o.Log {
	case "\x00help":
		fmt.Print(cli.Help())
		return 0
	case "\x00version":
		fmt.Printf("mins %s\nparity: %s\n", cli.Version, cli.Parity)
		return 0
	}
	if len(o.Positional) == 0 {
		fmt.Fprint(os.Stderr, cli.Help())
		fmt.Fprintln(os.Stderr, "mins: error: at least one input file is required")
		return 2
	}

	logPath := o.Log
	if logPath == "auto" {
		logPath = filepath.Join(filepath.Dir(o.Positional[0]), "mins.log")
	}
	log, err := logx.Open(logPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mins: error: cannot write log file: "+err.Error())
		return 2
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		core.Cleanup()
		_ = log.Close()
		os.Exit(130)
	}()

	if log.Enabled() {
		cwd, _ := os.Getwd()
		log.Header(cli.Version, o.Args, cwd)
	}

	c, err := config.Normalize(o)
	if err != nil {
		log.Error("%s", err)
		core.Cleanup()
		_ = log.Close()
		return 2
	}

	code := app.Run(c, log)
	core.Cleanup()
	_ = log.Close()
	return code
}
