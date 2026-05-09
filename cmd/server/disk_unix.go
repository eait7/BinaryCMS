//go:build !windows
package main

import "syscall"

func getDiskSpace() (total, used, free uint64, percent float64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err == nil {
		total = stat.Blocks * uint64(stat.Bsize)
		free = stat.Bfree * uint64(stat.Bsize)
		used = total - free
		if total > 0 {
			percent = float64(used) / float64(total) * 100
		}
	}
	return
}
