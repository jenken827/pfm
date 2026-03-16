package main

import (
	"jenken827.com/jenken827/pfm/bootstrap"
	"jenken827.com/jenken827/pfm/flags"
	"jenken827.com/jenken827/pfm/server"
)

func main() {
	flags.Parse()
	bootstrap.Init()
	server.Run()
}
