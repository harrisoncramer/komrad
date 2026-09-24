package main

import (
	"komrade/internal/bufcheck"

	"buf.build/go/bufplugin/check"
)

func main() {
	check.Main(bufcheck.Spec)
}
