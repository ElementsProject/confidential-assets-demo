// Copyright (c) 2017 DG Lab
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

/*
Package lib (cyclic.go) provides a very simple cyclic process management.

usage:
1) define a function for cyclic process. (no input/no output)
	ex) func loop() { fmt.Println("called."); return }
2) start that function
  a) wait for the processes will done.
	ex) _, err := lib.StartCyclic(loop, 3 , true)
  b) to do other task and wait SIGINT signal to stop.
    ex) wg, err := lib.StartCyclic(loop, 3, false)
	    // do something
	    wg.Wait()
  c) to do other task and stop the cyclic process immediately.
	ex) wg, err := lib.StartCyclic(loop, 3, false)
	    // do something
	    lib.StopCyclicProc(wg)
*/
package lib

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// StartCyclic calls each function with each interval.
func StartCyclic(callback func(), period int64, wait bool) (*sync.WaitGroup, error) {
	if period <= 0 {
		return nil, fmt.Errorf("period must be a plus value: %d", period)
	}

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		logger.Println("period", period, "s")
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT)
		ticker := time.NewTicker(time.Duration(period) * time.Second)

		defer func() {
			ticker.Stop()
			close(sig)
			wg.Done()
		}()

		for {
			select {
			case <-ticker.C:
				callback()
			case rcv := <-sig:
				logger.Println("signal:", rcv)
				return
			}
		}
	}()

	if wait {
		wg.Wait()
	}

	return wg, nil
}

// StopCyclicProc sends interrupt signal to myself.
func StopCyclicProc(wg *sync.WaitGroup) {
	pid := os.Getpid()
	p, err := os.FindProcess(pid)
	if err == nil {
		_ = p.Signal(os.Interrupt)
	}
	if wg != nil {
		wg.Wait()
	}
}
