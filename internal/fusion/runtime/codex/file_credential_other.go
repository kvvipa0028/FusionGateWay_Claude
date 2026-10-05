//go:build !darwin && !linux

package codex

func readPrivateAuthFile(string) (privateAuthFile, error) { return privateAuthFile{}, ErrIdentity }
