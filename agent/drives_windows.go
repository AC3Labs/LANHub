//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

type Drive struct {
	Path  string `json:"path"`
	Label string `json:"label"`
	Free  uint64 `json:"free"`
	Total uint64 `json:"total"`
}

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procGetDiskFreeSpaceExW = kernel32.NewProc("GetDiskFreeSpaceExW")
)

func listDrives() []Drive {
	var results []Drive

	for letter := 'A'; letter <= 'Z'; letter++ {
		root := fmt.Sprintf("%c:\\", letter)

		ptr, err := syscall.UTF16PtrFromString(root)
		if err != nil {
			continue
		}

		var freeBytes, totalBytes, totalFreeBytes uint64

		ret, _, _ := procGetDiskFreeSpaceExW.Call(
			uintptr(unsafe.Pointer(ptr)),
			uintptr(unsafe.Pointer(&freeBytes)),
			uintptr(unsafe.Pointer(&totalBytes)),
			uintptr(unsafe.Pointer(&totalFreeBytes)),
		)

		if ret == 0 {
			continue
		}

		results = append(results, Drive{Path: root, Label: root, Free: freeBytes, Total: totalBytes})
	}

	return results
}
