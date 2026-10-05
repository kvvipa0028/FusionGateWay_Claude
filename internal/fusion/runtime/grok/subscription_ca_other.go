//go:build !darwin

package grok

// The verified root-owned system trust store is a macOS pilot boundary;
// other platforms fail closed instead of trusting an ambient store.
func readSubscriptionCA() (subscriptionCA, error) {
	return subscriptionCA{}, ErrSubscriptionTransport
}
