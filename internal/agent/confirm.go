package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// confirmTimeout bounds how long a send waits for the person. A window nobody is sitting at must
// not hold an agent forever, and an agent's own timeout is shorter than most people's patience.
const confirmTimeout = 2 * time.Minute

// ConfirmRequest is what the window is asked: an agent wants to send this, here, now.
type ConfirmRequest struct {
	ID        string `json:"id"`
	Agent     string `json:"agent"`
	Method    string `json:"method"`
	URL       string `json:"url"`
	Workspace string `json:"workspace"`
	Name      string `json:"name"`
}

// confirmations holds the sends that are waiting for an answer. The question is delivered as an
// event and the answer arrives on a channel of its own: the tool call is in flight while the window
// draws the question, and nothing polls.
type confirmations struct {
	mu      sync.Mutex
	pending map[string]chan bool
}

func newConfirmations() *confirmations {
	return &confirmations{pending: map[string]chan bool{}}
}

// ask publishes the question and waits for one of three answers: yes, no, or nobody said anything
// in time. Each ends the wait, and the entry never outlives the call it was made for.
func (c *confirmations) ask(
	ctx context.Context,
	ask ConfirmRequest,
	publish func(ConfirmRequest),
) (bool, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return false, err
	}
	id := hex.EncodeToString(raw)
	ask.ID = id

	answer := make(chan bool, 1)
	c.mu.Lock()
	c.pending[id] = answer
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	publish(ask)

	select {
	case allow := <-answer:
		return allow, nil
	case <-time.After(confirmTimeout):
		return false, errors.New("nobody confirmed this send in the app within two minutes")
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// answer delivers the window's decision. An id nobody is waiting for is a confirmation that already
// timed out — answering it is late rather than wrong, and it changes nothing.
func (c *confirmations) answer(id string, allow bool) {
	c.mu.Lock()
	answer, ok := c.pending[id]
	c.mu.Unlock()
	if !ok {
		return
	}
	select {
	case answer <- allow:
	default:
	}
}
