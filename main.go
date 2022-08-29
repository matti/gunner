package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

var desire string

var restart chan struct{}
var shutdown chan struct{}
var kill chan struct{}

var sigs chan os.Signal

func signalHandler(ctx context.Context) {
	var sigInts int

	for {
		select {
		case <-ctx.Done():
			return
		case sig := <-sigs:
			switch sig {
			case syscall.SIGTSTP:
			case syscall.SIGTERM:
				desire = "shutdown"
				shutdown <- struct{}{}
			case syscall.SIGINT:
				sigInts++
			}
		case <-time.After(time.Millisecond * 200):
			switch sigInts {
			case 0:
			case 1:
				desire = "restart"
				restart <- struct{}{}
			case 2, 3, 4:
				desire = "shutdown"
				shutdown <- struct{}{}
			default:
				desire = "kill"
				kill <- struct{}{}
			}
			sigInts = 0
		}
	}
}

func run(ctx context.Context) *exec.Cmd {
	recoverable := false
	defer func() {
		r := recover()
		if r == nil {
			return
		}

		if !recoverable {
			panic(r)
		}
	}()

	args := os.Args[1:]

	cmd := exec.Command(args[0], args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    0,
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	done := make(chan bool, 1)
	go func() {
		if err := cmd.Start(); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		cmd.Wait()
		done <- true
	}()

	running := true
	for running {
		select {
		case <-ctx.Done():
			go func() {
				time.Sleep(3 * time.Second)
				kill <- struct{}{}
			}()
			shutdown <- struct{}{}
		case <-done:
			running = false
			break
		case <-restart:
			shutdown <- struct{}{}
		case <-shutdown:
			recoverable = true
			cmd.Process.Signal(syscall.SIGTERM)
			recoverable = false
		case <-kill:
			recoverable = true
			cmd.Process.Signal(syscall.SIGKILL)
			recoverable = false
		}
	}

	return cmd
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs = make(chan os.Signal)
	signal.Notify(sigs)

	restart = make(chan struct{}, 1)
	shutdown = make(chan struct{}, 1)
	kill = make(chan struct{}, 1)

	desire = "restart"

	go signalHandler(ctx)

	var lastRunTook time.Duration
	for {
		start := time.Now()
		run(ctx)
		took := time.Since(start)

		switch desire {
		case "restart":
		case "shutdown", "kill":
			return
		default:
			log.Fatalln("unknown desire ", desire)
		}

		fmt.Printf("\n")
		if lastRunTook < time.Second {
			time.Sleep(time.Second)
		}

		lastRunTook = took
	}
}
