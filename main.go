package main

import "github.com/waygatetech/tix/cmd"

// version is stamped at release time by GoReleaser's default ldflags
// (-X main.version=...). Builds made any other way report "dev".
var version = "dev"

func main() {
	cmd.Execute(version)
}
