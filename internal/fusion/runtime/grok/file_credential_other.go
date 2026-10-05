//go:build !darwin && !linux

package grok

// Private descriptor-relative credential reads are only implemented for the
// unix pilot hosts; other platforms fail closed.
func readPrivateAuthFile(string) (privateAuthFile, error) {
	return privateAuthFile{}, ErrIdentity
}
