//go:build !fusion

package main

func fusionTaskCommand([]string) (bool, error) { return false, nil }
