// Copyright (c) 2017 DG Lab
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

// bob project main.go
package main

import (
	"fmt"
	"log"
	"os"

	"democonf"
	"lib"
	"rpc"
)

// Block with Transactions
type Block struct {
	Tx []string `json:"tx,"`
}

var rpcurl = "http://127.0.0.1:10010"
var rpcuser = "user"
var rpcpass = "pass"

var rpcClient *rpc.Rpc

var assets = make(map[string]string)

var logger *log.Logger

var blockcount = -1

func getblockcount() (int, error) {
	blockcount, res, err := rpcClient.RequestAndCastNumber("getblockcount")
	if err != nil {
		logger.Printf("Rpc#RequestAndCastNumber error:%v res:%+v", err, res)
		return -1, err
	}
	return int(blockcount), nil
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

func printtxouts(txid string) error {
	fmt.Println("TXID:", txid)
	var tx rpc.RawTransaction
	res, err := rpcClient.RequestAndUnmarshalResult(&tx, "getrawtransaction", txid, 1)
	if err != nil {
		logger.Printf("Rpc#RequestAndUnmarshalResult error:%v res:%+v", err, res)
		return err
	}
	format := "[%d] Value: %3v Asset: %7v -> %v\n"
	for _, out := range tx.Vout {
		if out.Asset == "" {
			fmt.Printf(format, out.N, "???", "???????", out.ScriptPubKey.Addresses)
		} else {
			if out.ScriptPubKey.Type == "fee" {
				fmt.Printf(format, out.N, out.Value, assets[out.Asset], "fee")
			} else {
				fmt.Printf(format, out.N, out.Value, assets[out.Asset], out.ScriptPubKey.Addresses)
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

func callback() {
	getassetlabels()
	blockheight, err := getblockcount()
	if err != nil {
		logger.Println("getblockcount error:", err)
		return
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

func loadConf() {
	conf := democonf.NewDemoConf("bob")
	rpcurl = conf.GetString("rpcurl", rpcurl)
	rpcuser = conf.GetString("rpcuser", rpcuser)
	rpcpass = conf.GetString("rpcpass", rpcpass)
}

func main() {
	logger = log.New(os.Stdout, "Bob:", log.LstdFlags+log.Lshortfile)
	fmt.Println("Bob starting")

	loadConf()
	rpcClient = rpc.NewRpc(rpcurl, rpcuser, rpcpass)

	lib.SetLogger(logger)
	lib.StartCyclic(callback, 3, true)

	fmt.Println("Bob stopping")
}
