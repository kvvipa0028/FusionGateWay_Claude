//go:build !darwin && !linux

package bootstrap

func controlRootIdentity(string) (fileIdentity, error) { return fileIdentity{}, ErrControlHost }
func openControlFiles(string) (fileIdentity, sourceRecord, error) {
	return fileIdentity{}, sourceRecord{}, ErrControlHost
}
