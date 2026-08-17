//go:build linux

package main

import (
	"syscall"
	"unsafe"
)

// makeRaw strips the line discipline off an already-open tty.
//
// The Linux spelling of `raw_bsd.go`: TCGETS/TCSETS, and a `Termios` whose flag
// fields are 32-bit. The flag names and the reasoning are identical — see
// `openRaw`. This exists so the tool compiles in CI, which does not have a badge
// plugged into it; the path that gets exercised on hardware is the BSD one.
func makeRaw(fd uintptr) error {
	var t syscall.Termios
	if _, _, err := syscall.Syscall6(syscall.SYS_IOCTL, fd, syscall.TCGETS,
		uintptr(unsafe.Pointer(&t)), 0, 0, 0); err != 0 {
		return err
	}

	t.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP |
		syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	t.Oflag &^= syscall.OPOST
	t.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	t.Cflag &^= syscall.CSIZE | syscall.PARENB
	t.Cflag |= syscall.CS8 | syscall.CLOCAL | syscall.CREAD
	t.Cc[syscall.VMIN] = 0
	t.Cc[syscall.VTIME] = 0

	if _, _, err := syscall.Syscall6(syscall.SYS_IOCTL, fd, syscall.TCSETS,
		uintptr(unsafe.Pointer(&t)), 0, 0, 0); err != 0 {
		return err
	}
	return nil
}
