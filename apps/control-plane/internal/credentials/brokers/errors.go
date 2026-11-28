package brokers

// answerError is a store's answer outside 2xx. Its message names only the
// status, as before; body keeps the start of the store's answer for a broker
// that must tell one refusal from another, such as Doppler's answer for a
// token already deleted.
type answerError struct {
	status int
	body   []byte
	err    error
}

func (e *answerError) Error() string { return e.err.Error() }
func (e *answerError) Unwrap() error { return e.err }
