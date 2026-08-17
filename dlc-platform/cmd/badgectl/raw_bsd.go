//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package main

import (
	"syscall"
	"unsafe"
)

// makeRaw strips the line discipline off an already-open tty.
//
// The BSD spelling: TIOCGETA/TIOCSETA rather than Linux's TCGETS/TCSETS. Both
// are in the standard library's syscall package, which is why this tool still
// needs no serial dependency for one flag — see `openRaw` for what the flags are
// protecting against.
func makeRaw(fd uintptr) error {
	var t syscall.Termios
	if _, _, err := syscall.Syscall6(syscall.SYS_IOCTL, fd, syscall.TIOCGETA,
		uintptr(unsafe.Pointer(&t)), 0, 0, 0); err != 0 {
		return err
	}

	// The four groups that rewrite bytes, all cleared. ONLCR and ICRNL are the
	// ones that corrupt a binary frame; IXON is the one that makes bytes vanish.
	t.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP |
		syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	t.Oflag &^= syscall.OPOST
	t.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	t.Cflag &^= syscall.CSIZE | syscall.PARENB
	// CLOCAL: do not wait on carrier. Without it a reopen can block forever on a
	// device that never asserts DCD, which a USB CDC port has no reason to.
	t.Cflag |= syscall.CS8 | syscall.CLOCAL | syscall.CREAD

	// Return whatever has arrived, immediately. The caller polls with a read
	// deadline, so blocking in the kernel for a minimum byte count would fight it.
	t.Cc[syscall.VMIN] = 0
	t.Cc[syscall.VTIME] = 0

	if _, _, err := syscall.Syscall6(syscall.SYS_IOCTL, fd, syscall.TIOCSETA,
		uintptr(unsafe.Pointer(&t)), 0, 0, 0); err != 0 {
		return err
	}
	return nil
}
