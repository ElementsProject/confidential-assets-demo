// Copyright (c) 2017 DG Lab
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

// fred project main.go
package main

import (
	"fmt"
	"log"
	"os"

	"democonf"
	"lib"
	"rpc"
)

// URL for accessing RPC
var rpcurl = "http://127.0.0.1:10040"

// ID for accessing RPC
var rpcuser = "user"

// Password for accessing RPC
var rpcpass = "pass"

var rpcClient *rpc.Rpc

var logger *log.Logger

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

func callback() {
	err := checkgenerate()
	if err != nil {
		logger.Println("checkgenarate error:", err)
	}
}

func loadConf() {
	conf := democonf.NewDemoConf("fred")
	rpcurl = conf.GetString("rpcurl", rpcurl)
	rpcuser = conf.GetString("rpcuser", rpcuser)
	rpcpass = conf.GetString("rpcpass", rpcpass)
}

func main() {
	logger = log.New(os.Stdout, "Fred:", log.LstdFlags+log.Lshortfile)
	fmt.Println("Fred start")

	loadConf()
	rpcClient = rpc.NewRpc(rpcurl, rpcuser, rpcpass)

	lib.SetLogger(logger)
	lib.StartCyclic(callback, 3, true)

	fmt.Println("Fred stop")
}
