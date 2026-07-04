package runner

// MultiSink fans every event out to multiple downstream sinks. Useful for
// combining the live TUI with a post-run summary printer.
type MultiSink struct {
	sinks []Sink
}

// NewMultiSink returns a Sink that delegates to each of the given sinks.
func NewMultiSink(sinks ...Sink) *MultiSink {
	return &MultiSink{sinks: sinks}
}

// Emit forwards ev to every sink.
func (m *MultiSink) Emit(ev Event) {
	for _, s := range m.sinks {
		s.Emit(ev)
	}
}

// Close closes every sink; the first error is returned and the rest are
// attempted regardless.
func (m *MultiSink) Close() error {
	var firstErr error
	for _, s := range m.sinks {
		if err := s.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
