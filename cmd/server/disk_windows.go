//go:build windows
package main

func getDiskSpace() (total, used, free uint64, percent float64) {
	return 0, 0, 0, 0
}
