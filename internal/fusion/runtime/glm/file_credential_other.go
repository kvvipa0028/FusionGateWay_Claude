//go:build !darwin && !linux

package glm

func readCredentialFile(string) (credentialRecord, error) { return credentialRecord{}, ErrUnsupported }
