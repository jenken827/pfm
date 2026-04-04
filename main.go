package main

import (
	"jenken827.com/jenken827/pfm/bootstrap"
	_ "jenken827.com/jenken827/pfm/driver"
	"jenken827.com/jenken827/pfm/globals"
	"jenken827.com/jenken827/pfm/server"
)

func main() {
	globals.Parse()
	bootstrap.Init()
	server.Run()
}
