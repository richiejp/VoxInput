package openairt

import "log"

type Logger interface {
	Debugf(format string, v ...any)
	Infof(format string, v ...any)
	Warnf(format string, v ...any)
	Errorf(format string, v ...any)
}

// NopLogger is a logger that does nothing.
type NopLogger struct{}

// Errorf does nothing.
func (l NopLogger) Errorf(_ string, _ ...any) {}

// Warnf does nothing.
func (l NopLogger) Warnf(_ string, _ ...any) {}

// Infof does nothing.
func (l NopLogger) Infof(_ string, _ ...any) {}

// Debugf does nothing.
func (l NopLogger) Debugf(_ string, _ ...any) {}

// StdLogger is a logger that logs to the "log" package.
type StdLogger struct{}

func (l StdLogger) Errorf(format string, v ...any) {
	log.Printf("[ERROR] "+format, v...)
}

func (l StdLogger) Warnf(format string, v ...any) {
	log.Printf("[WARN] "+format, v...)
}

func (l StdLogger) Infof(format string, v ...any) {
	log.Printf("[INFO] "+format, v...)
}

func (l StdLogger) Debugf(format string, v ...any) {
	log.Printf("[DEBUG] "+format, v...)
}
