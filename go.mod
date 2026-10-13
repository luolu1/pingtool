module pingtool

// Pinned to Go 1.20: it is the last Go release that supports Windows 7 / 8 /
// Server 2008 R2 / 2012 / 2012 R2 (Go 1.21+ requires Windows 10 / Server 2016).
// The dependencies below are pinned to the newest versions that still declare
// an older Go language version so this module can be built with the Go 1.20
// toolchain used in the release workflow.
go 1.20

require (
	github.com/lxn/walk v0.0.0-20210112085537-c389da54e794
	golang.org/x/net v0.35.0
)

require (
	github.com/lxn/win v0.0.0-20210218163916-a377121e959e // indirect
	golang.org/x/sys v0.30.0 // indirect
	gopkg.in/Knetic/govaluate.v3 v3.0.0 // indirect
)
