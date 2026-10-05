//go:build !darwin

package evidence

import "context"

func available() bool                                   { return false }
func sandbox(string, string, string) (string, []string) { return "", nil }
func execute(context.Context, string, []string, string, string, []string, int, func() bool) execution {
	return execution{exit: -1}
}
