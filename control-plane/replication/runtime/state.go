package runtime

// State describes the externally visible lifecycle of a replication runtime.
type State string

const (
	StateCreated  State = "created"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateClosed   State = "closed"
	StateFailed   State = "failed"
)

func (r *Runtime) stateValue() State {
	if r == nil {
		return StateFailed
	}
	value, ok := r.state.Load().(string)
	if !ok || value == "" {
		return StateCreated
	}
	return State(value)
}

func (r *Runtime) State() State { return r.stateValue() }

func (r *Runtime) Ready() bool { return r.stateValue() == StateRunning }

func (r *Runtime) setState(state State) { r.state.Store(string(state)) }
