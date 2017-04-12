// Copyright (c) 2017 DG Lab
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package main

import (
	"democonf"
	"encoding/json"
	"fmt"
	"lib"
	"log"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"rpc"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type exchangeRateTuple struct {
	Rate float64 `json:"rate"`
	Min  int64   `json:"min"`
	Max  int64   `json:"max"`
	Unit int64   `json:"unit"`
	Fee  int64   `json:"fee,"`
}

const (
	myActorName      = "charlie"
	defaultRateFrom  = "AKISKY"
	defaultRateTo    = "MELON"
	defaultRPCURL    = "http://127.0.0.1:10020"
	defaultRPCUser   = "user"
	defaultRPCPass   = "pass"
	defaultLocalAddr = ":8020"
	defaultTxPath    = "elements-tx"
	defaultTxOption  = ""
	defaultTimeout   = 600
)

var logger = log.New(os.Stdout, myActorName+":", log.LstdFlags+log.Lshortfile)
var conf = democonf.NewDemoConf(myActorName)
var assetIDMap = make(map[string]string)
var lockList = make(rpc.LockList)
var rpcClient *rpc.Rpc
var elementsTxCommand string
var elementsTxOption string
var localAddr string
var defaultRateTuple = exchangeRateTuple{Rate: 0.5, Min: 100, Max: 200000, Unit: 20, Fee: 15}
var fixedRateTable = make(map[string](map[string]exchangeRateTuple))

var handlerList = map[string]func(url.Values, string) ([]byte, error){
	"/getexchangerate/":    doGetRate,
	"/getexchangeofferwb/": doOfferWithBlinding,
	"/getexchangeoffer/":   doOffer,
	"/submitexchange/":     doSubmit,
}

var cyclics = []lib.CyclicProcess{lib.CyclicProcess{Handler: lockList.Sweep, Interval: 3}}

func doGetRate(reqParam url.Values, reqBody string) ([]byte, error) {
	var requestAsset string
	var requestAmount int64
	var rateRequest lib.ExchangeRateRequest

	err := json.Unmarshal([]byte(reqBody), &rateRequest)
	if err != nil {
		logger.Println("json#Unmarshal error:", err)
		return nil, err
	}
	request := rateRequest.Request
	if len(request) != 1 {
		err = fmt.Errorf("request must be a single record but has:%d", len(request))
		logger.Println("error:", err)
		return nil, err
	}
	for k, v := range request {
		requestAsset = k
		requestAmount = v
	}

	// 1. lookup config
	rateRes, err := lookupRate(requestAsset, requestAmount, rateRequest.Offer)
	if err != nil {
		logger.Println("error:", err)
		b, _ := json.Marshal(fmt.Sprintf("%v", err))
		return b, nil
	}

	b, err := json.Marshal(rateRes)
	if err != nil {
		logger.Println("json#Marshal error:", err)
		return nil, err
	}

	logger.Println("<<" + string(b))
	return b, nil
}

func doOfferWithBlinding(reqParam url.Values, reqBody string) ([]byte, error) {
	var requestAsset string
	var requestAmount int64
	var offerRequest lib.ExchangeOfferWBRequest

	err := json.Unmarshal([]byte(reqBody), &offerRequest)
	if err != nil {
		logger.Println("json#Unmarshal error:", err)
		return nil, err
	}
	request := offerRequest.Request
	if len(request) != 1 {
		err = fmt.Errorf("request must be a single record but has:%d", len(request))
		logger.Println("error:", err)
		return nil, err
	}
	for k, v := range request {
		requestAsset = k
		requestAmount = v
	}

	offer := offerRequest.Offer
	commitments := offerRequest.Commitments

	// 1. lookup rate
	offerRes, err := lookupRate(requestAsset, requestAmount, offer)
	if err != nil {
		logger.Println("error:", err)
		b, _ := json.Marshal(fmt.Sprintf("%v", err))
		return b, nil
	}

	// 2. lookup unspent
	utxos, err := rpcClient.SearchUnspent(lockList, requestAsset, requestAmount, true)
	if err != nil {
		logger.Println("error:", err)
		return nil, err
	}
	rautxos, err := rpcClient.SearchMinimalUnspent(lockList, offer, true)
	if err != nil {
		logger.Println("error:", err)
		return nil, err
	}

	// 3. creat tx
	tx, err := createTransactionTemplateWB(requestAsset, requestAmount, offer, offerRes, utxos, rautxos)
	if err != nil {
		logger.Println("error:", err)
		return nil, err
	}

	// 4. blinding
	cmutxos := append(utxos, rautxos...)
	resCommitments, err := rpcClient.GetCommitments(cmutxos)
	if err != nil {
		logger.Println("error:", err)
		return nil, err
	}
	commitments = append(resCommitments, commitments...)

	blindtx, _, err := rpcClient.RequestAndCastString("blindrawtransaction", tx, commitments)
	if err != nil {
		logger.Println("RPC/blindrawtransaction error:", err, tx)
		return nil, err
	}

	offerWBRes := lib.ExchangeOfferWBResponse{
		Fee:         offerRes.Fee,
		AssetLabel:  offerRes.AssetLabel,
		Cost:        offerRes.Cost,
		Transaction: blindtx,
		Commitments: resCommitments,
	}

	b, err := json.Marshal(offerWBRes)
	if err != nil {
		logger.Println("json#Marshal error:", err)
		return nil, err
	}

	logger.Println("<<" + string(b))
	return b, nil
}

func doOffer(reqParam url.Values, reqBody string) ([]byte, error) {
	var requestAsset string
	var requestAmount int64
	var offerRequest lib.ExchangeOfferRequest

	err := json.Unmarshal([]byte(reqBody), &offerRequest)
	if err != nil {
		logger.Println("json#Unmarshal error:", err)
		return nil, err
	}
	request := offerRequest.Request
	if len(request) != 1 {
		err = fmt.Errorf("request must be a single record but has:%d", len(request))
		logger.Println("error:", err)
		return nil, err
	}
	for k, v := range request {
		requestAsset = k
		requestAmount = v
	}

	offer := offerRequest.Offer

	// 1. lookup config
	offerRes, err := lookupRate(requestAsset, requestAmount, offer)
	if err != nil {
		logger.Println("error:", err)
		b, _ := json.Marshal(fmt.Sprintf("%v", err))
		return b, nil
	}

	// 2. lookup unspent
	utxos, err := rpcClient.SearchUnspent(lockList, requestAsset, requestAmount, false)
	if err != nil {
		logger.Println("error:", err)
		return nil, err
	}

	// 3. creat tx
	offerRes.Transaction, err = createTransactionTemplate(requestAsset, requestAmount, offer, offerRes.Cost, utxos)
	if err != nil {
		logger.Println("error:", err)
		return nil, err
	}

	b, err := json.Marshal(offerRes)
	if err != nil {
		logger.Println("json#Marshal error:", err)
		return nil, err
	}

	logger.Println("<<" + string(b))
	return b, nil
}

func createTransactionTemplate(requestAsset string, requestAmount int64, offer string, cost int64, utxos rpc.UnspentList) (string, error) {
	change := utxos.GetAmount() - requestAmount

	addrOffer, err := rpcClient.GetNewAddr(false)
	if err != nil {
		return "", err
	}

	params := []string{}

	if elementsTxOption != "" {
		params = append(params, elementsTxOption)
	}
	params = append(params, "-create")

	for _, u := range utxos {
		txin := "in=" + u.Txid + ":" + strconv.FormatInt(u.Vout, 10)
		params = append(params, txin)
	}

	outAddrOffer := "outaddr=" + strconv.FormatInt(cost, 10) + ":" + addrOffer + ":" + assetIDMap[offer]
	params = append(params, outAddrOffer)

	if 0 < change {
		addrChange, err := rpcClient.GetNewAddr(false)
		if err != nil {
			return "", err
		}
		outAddrChange := "outaddr=" + strconv.FormatInt(change, 10) + ":" + addrChange + ":" + assetIDMap[requestAsset]
		params = append(params, outAddrChange)
	}

	out, err := exec.Command(elementsTxCommand, params...).Output()

	if err != nil {
		logger.Println("elements-tx error:", err, params, out)
		return "", err
	}

	txTemplate := strings.TrimRight(string(out), "\n")
	return txTemplate, nil
}

func createTransactionTemplateWB(requestAsset string, requestAmount int64, offer string, offerRes lib.ExchangeOfferResponse, utxos rpc.UnspentList, loopbackUtxos rpc.UnspentList) (string, error) {
	change := utxos.GetAmount() - requestAmount
	lbChange := loopbackUtxos.GetAmount() + offerRes.Cost

	addrOffer, err := rpcClient.GetNewAddr(true)
	if err != nil {
		return "", err
	}

	params := []string{}

	if elementsTxOption != "" {
		params = append(params, elementsTxOption)
	}
	params = append(params, "-create")

	for _, u := range utxos {
		txin := "in=" + u.Txid + ":" + strconv.FormatInt(u.Vout, 10)
		params = append(params, txin)
	}
	for _, u := range loopbackUtxos {
		txin := "in=" + u.Txid + ":" + strconv.FormatInt(u.Vout, 10)
		params = append(params, txin)
	}

	outAddrOffer := "outaddr=" + strconv.FormatInt(lbChange, 10) + ":" + addrOffer + ":" + assetIDMap[offer]
	params = append(params, outAddrOffer)

	if 0 < change {
		addrChange, err := rpcClient.GetNewAddr(true)
		if err != nil {
			return "", err
		}
		outAddrChange := "outaddr=" + strconv.FormatInt(change, 10) + ":" + addrChange + ":" + assetIDMap[requestAsset]
		params = append(params, outAddrChange)
	}
	outAddrFee := "outscript=" + strconv.FormatInt(offerRes.Fee, 10) + "::" + assetIDMap[offer]
	params = append(params, outAddrFee)

	out, err := exec.Command(elementsTxCommand, params...).Output()

	if err != nil {
		logger.Println("elements-tx error:", err, params, out)
		return "", err
	}

	txTemplate := strings.TrimRight(string(out), "\n")
	return txTemplate, nil
}

func doSubmit(rreqParam url.Values, reqBody string) ([]byte, error) {
	var rawTx rpc.RawTransaction
	var signedtx rpc.SignedTransaction

	var submitRequest lib.SubmitExchangeRequest

	err := json.Unmarshal([]byte(reqBody), &submitRequest)
	if err != nil {
		logger.Println("json#Unmarshal error:", err)
		return nil, err
	}
	rcvtx := submitRequest.Transaction

	_, err = rpcClient.RequestAndUnmarshalResult(&rawTx, "decoderawtransaction", rcvtx)
	if err != nil {
		logger.Println("RPC/decoderawtransaction error:", err, rcvtx)
		return nil, err
	}

	// TODO check rawTx (consistency with offer etc...)

	_, err = rpcClient.RequestAndUnmarshalResult(&signedtx, "signrawtransaction", rcvtx)
	if err != nil {
		logger.Println("RPC/signrawtransaction error:", err, rcvtx)
		return nil, err
	}

	txid, _, err := rpcClient.RequestAndCastString("sendrawtransaction", signedtx.Hex)
	if err != nil {
		logger.Println("RPC/sendrawtransaction error:", err, signedtx.Hex)
		return nil, err
	}

	submitRes := lib.SubmitExchangeResponse{
		TransactionId: txid,
	}

	for _, v := range rawTx.Vin {
		lockList.Unlock(v.Txid, v.Vout)
	}

	b, _ := json.Marshal(submitRes)
	logger.Println("<<" + string(b))
	return b, nil
}

func lookupRate(requestAsset string, requestAmount int64, offer string) (lib.ExchangeOfferResponse, error) {
	var offerRes lib.ExchangeOfferResponse

	rateMap, ok := fixedRateTable[offer]
	if !ok {
		err := fmt.Errorf("no exchange source:%s", offer)
		logger.Println("error:", err)
		return offerRes, err
	}

	rate, ok := rateMap[requestAsset]
	if !ok {
		err := fmt.Errorf("cannot exchange to:%s", requestAsset)
		logger.Println("error:", err)
		return offerRes, err
	}

	cost := int64(float64(requestAmount) / rate.Rate)
	if cost < rate.Min {
		err := fmt.Errorf("cost lower than min value:%d", cost)
		logger.Println("error:", err)
		return offerRes, err
	}
	if rate.Max < cost {
		err := fmt.Errorf("cost higher than max value:%d", cost)
		logger.Println("error:", err)
		return offerRes, err
	}

	offerRes = lib.ExchangeOfferResponse{
		Fee:         rate.Fee,
		AssetLabel:  offer,
		Cost:        cost,
		Transaction: "",
	}

	return offerRes, nil
}

func initialize() {
	logger = log.New(os.Stdout, myActorName+":", log.LstdFlags+log.Lshortfile)
	lib.SetLogger(logger)

	rpcClient = rpc.NewRpc(
		conf.GetString("rpcurl", defaultRPCURL),
		conf.GetString("rpcuser", defaultRPCUser),
		conf.GetString("rpcpass", defaultRPCPass))
	_, err := rpcClient.RequestAndUnmarshalResult(&assetIDMap, "dumpassetlabels")
	if err != nil {
		logger.Println("RPC/dumpassetlabels error:", err)
	}
	delete(assetIDMap, "bitcoin")

	localAddr = conf.GetString("laddr", defaultLocalAddr)
	elementsTxCommand = conf.GetString("txpath", defaultTxPath)
	elementsTxOption = conf.GetString("txoption", defaultTxOption)
	rpc.SetUtxoLockDuration(time.Duration(int64(conf.GetNumber("timeout", defaultTimeout))) * time.Second)
	fixedRateTable[defaultRateFrom] = map[string]exchangeRateTuple{defaultRateTo: defaultRateTuple}
	conf.GetInterface("fixrate", &fixedRateTable)
}

func main() {
	initialize()

	dir, _ := os.Getwd()
	listener, err := lib.StartHttpServer(localAddr, handlerList, dir+"/html/"+myActorName)
	defer listener.Close()
	if err != nil {
		logger.Println("error:", err)
		return
	}

	// signal handling (ctrl + c)
	sig := make(chan os.Signal)
	stop := false
	signal.Notify(sig, syscall.SIGINT)
	go func() {
		logger.Println(<-sig)
		stop = true
	}()

	lib.StartCyclicProc(cyclics, &stop)

	lib.WaitStopSignal(&stop)

	logger.Println(myActorName + " stop")
}
