package main

import (
	"strings"
	"unicode/utf8"
	"flag"
	"os"
	"fmt"
)

type Config struct {
	Cmdline string
	WorkDir string
	Key rune
}

var config Config

func main() {
	var tmp string
	flag.StringVar(&config.Cmdline, "exec", "cmd.exe", "exec name you want to run")
	flag.StringVar(&config.WorkDir, "work", "", "Working directory")
	flag.StringVar(&tmp, "hotkey", "", "Hotkey to run exec. You can run Ctrol+Alt+Hotkey")
	flag.Parse()

	if tmp == "" {
		fmt.Println("Please specify hotkey flag. It was empty.")
		os.Exit(-1)
	}

	config.Key,_ = utf8.DecodeRuneInString(strings.ToUpper(tmp))

	if config.Key < 'A' || config.Key > 'Z' {
		fmt.Println("Please specify hotkey flag between A to Z")
		os.Exit(-1)
	}

	run()
}


