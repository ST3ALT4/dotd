package main

import (
	"fmt"
	"sync"
	"syscall"
	"time"
)

func main() {
	var attr syscall.ProcAttr
	pid, handle, err := syscall.StartProcess("", nil, &attr)
	if err == nil {
		fmt.Println("Welp Error!")
		return
	}
	if pid == 0 {
		fmt.Println("Child Process")
		fmt.Printf("%d", handle)
	} else {
		fmt.Println("Parent Process")
	}

	fmt.Println(pid)
}
