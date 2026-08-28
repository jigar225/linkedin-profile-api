package server

import "sync"

// flightGroup coalesces concurrent requests for the same key ("singleflight"
// pattern): the first caller becomes the owner and runs the fetch; waiters
// block on the owner's completion and share its result. Prevents thundering
// herds on a cold cache (20 concurrent requests for one URL = 1 upstream fetch).
type flightGroup struct {
	mu    sync.Mutex
	calls map[string]*flightCall
}

// flightCall is one in-flight fetch. done closes when it finishes;
// data/err are valid to read after that (channel close = happens-before).
type flightCall struct {
	done chan struct{}
	data []byte
	err  error
}

// do runs fn for key, coalescing concurrent callers into one execution.
func (g *flightGroup) do(key string, fn func() ([]byte, error)) ([]byte, error) {
	g.mu.Lock()
	if g.calls == nil {
		g.calls = map[string]*flightCall{}
	}
	if c, ok := g.calls[key]; ok {
		// already in flight — wait for the owner's result
		g.mu.Unlock()
		<-c.done
		return c.data, c.err
	}
	c := &flightCall{done: make(chan struct{})}
	g.calls[key] = c
	g.mu.Unlock()

	c.data, c.err = fn()

	// delete + close under one lock: no window between them, so a request
	// arriving after this either becomes a new owner or missed nothing.
	g.mu.Lock()
	delete(g.calls, key)
	close(c.done)
	g.mu.Unlock()
	return c.data, c.err
}
