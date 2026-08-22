package main

import (
	"fmt"
	"os"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: yaegi <script.go> [args...]")
		os.Exit(2)
	}
	script := os.Args[1]
	os.Args = append([]string{script}, os.Args[2:]...)
	runner := interp.New(interp.Options{})
	if err := runner.Use(stdlib.Symbols); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := runner.EvalPath(script); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
