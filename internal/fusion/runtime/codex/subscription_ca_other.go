//go:build !darwin

package codex

// This delivery is the macOS pilot; other OS trust stores need their own proof.
func readSubscriptionCA() (subscriptionCA, error) {
	return subscriptionCA{}, ErrSubscriptionTransport
}
