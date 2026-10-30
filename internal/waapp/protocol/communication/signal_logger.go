package communication

import (
	"log"
	"sync"

	"go.mau.fi/libsignal/logger"
)

var signalLoggerOnce sync.Once

// SDK diagnostics can contain key material. Operation errors carry the public failure.
type signalLogger struct{}

func (signalLogger) Debug(string, string)   {}
func (signalLogger) Info(string, string)    {}
func (signalLogger) Warning(string, string) {}
func (signalLogger) Configure(string)       {}
func (signalLogger) Error(caller, _ string) { log.Printf("Signal operation failed at %s", caller) }

func ensureSignalLogger() {
	signalLoggerOnce.Do(func() {
		var adapter logger.Loggable = signalLogger{}
		logger.Setup(&adapter)
	})
}
