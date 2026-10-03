//go:build !fusion

package main

func fusionControlCommand([]string) (bool, error) { return false, nil }
