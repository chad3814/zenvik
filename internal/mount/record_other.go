//go:build !unix && !windows

package mount

// removeRecordCommand is empty: there is no known shell to target here.
func removeRecordCommand(string) string { return "" }
