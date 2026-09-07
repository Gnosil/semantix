//go:build darwin && cgo

package main

/*
#cgo darwin LDFLAGS: -framework Cocoa
void installSemantixSystemQuitHook(void);
*/
import "C"

import "sync"

var installSystemQuitHookOnce sync.Once

func installSystemQuitHook() {
	installSystemQuitHookOnce.Do(func() {
		C.installSemantixSystemQuitHook()
	})
}

//export SemantixMarkSystemQuit
func SemantixMarkSystemQuit() {
	markSystemQuitRequested()
}
