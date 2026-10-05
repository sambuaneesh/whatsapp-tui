package main

import (
	"net/http"
	_ "net/http/pprof" // profiles, only served when asked for
	"os"
)

// servePprof serves Go's profiles (memory, CPU) on
// $WHATSAPP_TUI_PPROF, e.g. 127.0.0.1:6060, for measuring the app. Off
// unless set.
func servePprof() {
	addr := os.Getenv("WHATSAPP_TUI_PPROF")
	if addr == "" {
		return
	}
	go func() { _ = http.ListenAndServe(addr, nil) }()
}
