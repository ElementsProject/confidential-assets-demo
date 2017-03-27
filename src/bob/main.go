// Copyright (c) 2017 DG Lab
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

// bob project main.go
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"democonf"
	"rpc"
)

// URL for accessing RPC
var rpcurl string = "http://127.0.0.1:10010"

// ID for accessing RPC
var rpcuser string = "user"

// Password for accessing RPC
var rpcpass string = "pass"

var rpcClient *rpc.Rpc

var interval = 3 * time.Second

var assets = make(map[string]string)

var logger *log.Logger

var stop bool = false

func getblockcount() (int, error) {
	blockcount, res, err := rpcClient.RequestAndCastNumber("getblockcount")
	if err != nil {
		logger.Printf("Rpc#RequestAndCastNumber error:%v res:%+v", err, res)
		return -1, err
	}
	return int(blockcount), nil
}

type Block struct {
	Tx []string `json:"tx,"`
}

func viewBlock(height int) error {
	blockhash, res, err := rpcClient.RequestAndCastString("getblockhash", height)
	if err != nil {
		logger.Printf("Rpc#RequestAndCastString error:%v res:%+v", err, res)
		return err
	}
	var block Block
	res, err = rpcClient.RequestAndUnmarshalResult(&block, "getblock", blockhash)
	if err != nil {
		logger.Printf("Rpc#RequestAndUnmarshalResult error:%v res:%+v", err, res)
		return err
	}
	for _, tx := range block.Tx {
		printtxouts(fmt.Sprintf("%v", tx))
	}
	return nil
}

type Transaction struct {
	Vout []Output `json:"vout,"`
}

type Output struct {
	Value        float64                `json:"value,"`
	Fee          float64                `json:"fee_value,"`
	Assetid      string                 `json:"assetid,"`
	Assettag     string                 `json:"assettag,"`
	N            int                    `json:"n,"`
	ScriptPubKey map[string]interface{} `json:"scriptPubKey,"`
}

func printtxouts(txid string) error {
	fmt.Println("TXID:", txid)
	var tx Transaction
	res, err := rpcClient.RequestAndUnmarshalResult(&tx, "getrawtransaction", txid, 1)
	if err != nil {
		logger.Printf("Rpc#RequestAndUnmarshalResult error:%v res:%+v", err, res)
		return err
	}
	format := "[%d] Value: %v Asset: %v -> %v\n"
	for _, out := range tx.Vout {
		if out.Assetid == "" {
			fmt.Printf(format, out.N, "???", "???", out.ScriptPubKey["addresses"])
		} else {
			if out.Value == 0 {
				fmt.Printf(format, out.N, out.Fee, assets[out.Assetid], "fee")
			} else {
				fmt.Printf(format, out.N, out.Value, assets[out.Assetid], out.ScriptPubKey["addresses"])
			}
		}
	}
	return nil
}

func getassetlabels() error {
	var labels map[string]string
	res, err := rpcClient.RequestAndUnmarshalResult(&labels, "dumpassetlabels")
	if err != nil {
		logger.Printf("Rpc#RequestAndUnmarshalResult error:%v res:%+v", err, res)
		return err
	}
	for k, v := range labels {
		assets[fmt.Sprintf("%v", v)] = k
	}
	return nil
}

func loop() {
	fmt.Println("Loop interval:", interval)
	blockcount := -1
	for {
		time.Sleep(interval)
		if stop {
			break
		}
		getassetlabels()
		blockheight, err := getblockcount()
		if err != nil {
			logger.Println("getblockcount error:", err)
			continue
		}
		if blockcount < 0 {
			blockcount = blockheight
			fmt.Println("Start block", blockcount)
		} else if blockcount < blockheight {
			for blockcount < blockheight {
				blockcount++
				fmt.Println("Find block", blockcount)
				viewBlock(blockcount)
			}
		}
	}
}

func loadConf() {
	conf := democonf.NewDemoConf("bob")
	rpcurl = conf.GetString("rpcurl", rpcurl)
	rpcuser = conf.GetString("rpcuser", rpcuser)
	rpcpass = conf.GetString("rpcpass", rpcpass)
}

func main() {
	logger = log.New(os.Stdout, "Bob:", log.LstdFlags+log.Lshortfile)
	fmt.Println("Bob start")

	loadConf()
	rpcClient = rpc.NewRpc(rpcurl, rpcuser, rpcpass)

	// signal handling (ctrl + c)
	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT)
	go func() {
		fmt.Println(<-sig)
		stop = true
	}()

	loop()
	fmt.Println("Bob stop")
}
