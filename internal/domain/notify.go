package domain

// Notifier is where a feature publishes what it wants the window told. It is declared here rather
// than in each of the features that need one: the interface is two words — a topic and a payload —
// and it was the same two words six times over.
type Notifier interface {
	Publish(topic string, payload any)
}
