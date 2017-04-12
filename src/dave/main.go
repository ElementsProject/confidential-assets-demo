// Copyright (c) 2017 DG Lab
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

// dave project main.go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"democonf"
	"rpc"
)

// URL for accessing RPC
var rpcurl = "http://127.0.0.1:10030"

// ID for accessing RPC
var rpcuser = "user"

// Password for accessing RPC
var rpcpass = "pass"

// Listen addr for RPC Proxy
var laddr = ":8030"

//  getNewAddress use confidential
var confidential = false

var rpcClient *rpc.Rpc

var interval = 3 * time.Second

// Item details
type Item struct {
	Price   float64
	Asset   string
	Timeout int64
}

var items = map[string]Item{
	"Caramel Macchiato Coffee": Item{Price: float64(200), Asset: "MELON", Timeout: int64(60 * 60)},
}

// Order details
type Order struct {
	Item       string
	Addr       string
	Status     int
	Asset      string
	Price      float64
	Timeout    int64
	LastModify int64
}

var list = []*Order{}

var logger *log.Logger

var stop = false

func loop() {
	fmt.Println("Loop interval:", interval)
	for {
		time.Sleep(interval)
		if stop {
			break
		}
		for _, order := range list {
			if order.Status != 0 {
				continue
			}
			now := time.Now().Unix()
			if order.Timeout <= now {
				fmt.Println("Timeout!", order.Addr)
				order.Status = -1
				order.LastModify = now
				continue
			}
			amount, res, err := rpcClient.RequestAndCastNumber("getreceivedbyaddress", order.Addr, 1, order.Asset)
			if err != nil {
				logger.Printf("Rpc#RequestAndCastNumber error:%v res:%+v", err, res)
				continue
			}
			if amount >= order.Price {
				fmt.Println("Paid!", order.Addr)
				order.Status = 1
				order.LastModify = now
			}
		}
	}
}

func orderhandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Access-Control-Allow-Origin", "*")
	w.Header().Add("Access-Control-Allow-Methods", "POST,GET")
	w.Header().Add("Access-Control-Allow-Headers", r.Header.Get("Access-Control-Request-Headers"))
	w.Header().Add("Access-Control-Max-Age", "-1")

	item := r.FormValue("item")
	result := make(map[string]interface{})
	result["result"] = false
	for key, val := range items {
		if key == item {
			addr, err := rpcClient.GetNewAddr(confidential)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				logger.Println("getNewAddress error", err)
				return
			}
			result["result"] = true
			vals := url.Values{}
			result["name"] = key
			vals["name"] = []string{key}
			result["addr"] = addr
			vals["addr"] = []string{addr}
			result["price"] = val.Price
			vals["price"] = []string{fmt.Sprintf("%v", val.Price)}
			result["asset"] = val.Asset
			vals["asset"] = []string{val.Asset}
			uri := fmt.Sprint("px:invoice?", vals.Encode())
			fmt.Println(uri)
			result["uri"] = uri
			now := time.Now().Unix()
			order := &Order{Item: key, Addr: addr, Status: 0, Timeout: now + val.Timeout, Price: val.Price, Asset: val.Asset, LastModify: now}
			list = append(list, order)
			bs, _ := json.Marshal(order)
			fmt.Println("Order", string(bs))
			break
		}
	}
	if !result["result"].(bool) {
		w.WriteHeader(http.StatusNotFound)
		result["error"] = fmt.Sprint("Not Found item. ", item)
	}
	bs, _ := json.Marshal(result)
	w.Write(bs)
}

func listhandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Access-Control-Allow-Origin", "*")
	w.Header().Add("Access-Control-Allow-Methods", "POST,GET")
	w.Header().Add("Access-Control-Allow-Headers", r.Header.Get("Access-Control-Request-Headers"))
	w.Header().Add("Access-Control-Max-Age", "-1")

	res := make(map[string]interface{})
	res["result"] = list
	bs, _ := json.Marshal(res)
	w.Write(bs)
}

func loadConf() {
	conf := democonf.NewDemoConf("dave")
	rpcurl = conf.GetString("rpcurl", rpcurl)
	rpcuser = conf.GetString("rpcuser", rpcuser)
	rpcpass = conf.GetString("rpcpass", rpcpass)
	laddr = conf.GetString("laddr", laddr)
	confidential = conf.GetBool("confidential", confidential)
}

func main() {
	logger = log.New(os.Stdout, "Dave:", log.LstdFlags+log.Lshortfile)
	fmt.Println("Dave starting")

	loadConf()
	rpcClient = rpc.NewRpc(rpcurl, rpcuser, rpcpass)

	listener, err := net.Listen("tcp", laddr)
	if err != nil {
		logger.Println("net#Listen error:", err)
		return
	}
	defer listener.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/order", orderhandler)
	mux.HandleFunc("/list", listhandler)
	dir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	fmt.Println("html path:", http.Dir(dir+"/html/dave"))
	mux.Handle("/", http.FileServer(http.Dir(dir+"/html/dave")))
	fmt.Println("start listening...", listener.Addr().Network(), listener.Addr())
	go http.Serve(listener, mux)

	// signal handling (ctrl + c)
	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT)
	go func() {
		fmt.Println(<-sig)
		stop = true
	}()

	loop()

	fmt.Println("Dave stopping")
}
