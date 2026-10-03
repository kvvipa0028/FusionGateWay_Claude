//go:build !darwin && !linux

package bootstrap

func readSource(string) (sourceRecord, error) { return sourceRecord{}, ErrRegistration }
func readFolder(string) (fileIdentity, error) { return fileIdentity{}, ErrRegistration }
