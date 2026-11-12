package agent

import "errors"

var errPrecondition = errors.New("工程已变化，请重新读取后再保存")

// compareAndLoad checks and replaces within the same lock. A preflight HTTP
// check alone cannot prevent two writers from overwriting each other.
func (w *Workspace) compareAndLoad(expected Binding, p Project) (Binding, error) {
	if err := Validate(p); err != nil {
		return Binding{}, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.binding() != expected {
		return Binding{}, errPrecondition
	}
	next := Clone(p)
	w.project = &next
	w.epoch++
	w.resetHistory()
	return w.binding(), nil
}
