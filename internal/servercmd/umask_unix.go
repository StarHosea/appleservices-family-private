//go:build !windows

package servercmd

import "syscall"

func secureDefaults() { syscall.Umask(0077) }
