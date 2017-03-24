// Copyright (c) 2017 DG Lab
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

// fred project main.go
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"rpc"
)

// URL for accessing RPC
var url string = "http://127.0.0.1:10040"

// ID for accessing RPC
var rpcuser string = "user"

// Password for accessing RPC
var rpcpass string = "pass"

var rpcClient *rpc.Rpc = rpc.NewRpc(url, rpcuser, rpcpass)

var interval = 3 * time.Second

var logger *log.Logger

var stop bool = false

func checkgenerate() error {
	var txs []string
	res, err := rpcClient.RequestAndUnmarshalResult(&txs, "getrawmempool")
	if err != nil {
		logger.Printf("Rpc#RequestAndUnmarshalResult error:%v res:%+v", err, res)
		return err
	}
	if len(txs) == 0 {
		return nil
	}
	rpcClient.View = true
	var hashs []string
	res, err = rpcClient.RequestAndUnmarshalResult(&hashs, "generate", 1)
	rpcClient.View = false
	if err != nil {
		logger.Printf("Rpc#RequestAndUnmarshalResult error:%v res:%+v", err, res)
		return err
	}
	return nil
}

func loop() {
	fmt.Println("Loop interval:", interval)
	for {
		time.Sleep(interval)
		err := checkgenerate()
		if err != nil {
			logger.Println("checkgenarate error:", err)
		}
		if stop {
			break
		}
	}
}

func main() {
	logger = log.New(os.Stdout, "Fred:", log.LstdFlags+log.Lshortfile)
	fmt.Println("Fred start")

	// signal handling (ctrl + c)
	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT)
	go func() {
		fmt.Println("signal:", <-sig)
		stop = true
	}()

	loop()

	fmt.Println("Fred stop")
}
