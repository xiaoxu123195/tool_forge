//go:build darwin && cgo

package main

/*
#cgo LDFLAGS: -framework UniformTypeIdentifiers
*/
import "C"
