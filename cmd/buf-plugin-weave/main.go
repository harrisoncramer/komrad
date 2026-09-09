package main

import (
	"weave/internal/bufcheck"

	"buf.build/go/bufplugin/check"
)

func main() {
	check.Main(bufcheck.Spec)
}
