package logging

// Nop returns a Logger that discards everything, for code that needs a
// Logger but has nothing to report, such as describing an API.
func Nop() Logger { return nop{} }

type nop struct{}

func (nop) Debug(string, ...Field) {}
func (nop) Info(string, ...Field)  {}
func (nop) Warn(string, ...Field)  {}
func (nop) Error(string, ...Field) {}
func (nop) Fatal(string, ...Field) {}
func (nop) Sync() error            { return nil }
