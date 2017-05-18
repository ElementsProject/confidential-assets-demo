// Copyright (c) 2017 DG Lab
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package main

import (
	"democonf"
	"fmt"
	"lib"
	"log"
	"os"
	"os/exec"
	"rpc"
	"strconv"
	"strings"
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

var handlerList = map[string]interface{}{
	"/getexchangerate/":    doGetRate,
	"/getexchangeofferwb/": doOfferWithBlinding,
	"/getexchangeoffer/":   doOffer,
	"/submitexchange/":     doSubmit,
}

func doGetRate(rateRequest lib.ExchangeRateRequest) (lib.ExchangeRateResponse, error) {
	var rateRes lib.ExchangeRateResponse
	var requestAsset string
	var requestAmount int64
	var err error

	request := rateRequest.Request
	if len(request) != 1 {
		err = fmt.Errorf("request must be a single record but has:%d", len(request))
		logger.Println("error:", err)
		return rateRes, err
	}
	for k, v := range request {
		requestAsset = k
		requestAmount = v
	}

	// 1. lookup config
	rateRes, err = lookupRate(requestAsset, requestAmount, rateRequest.Offer)
	if err != nil {
		logger.Println("error:", err)
	}

	return rateRes, err
}

func doOfferWithBlinding(offerRequest lib.ExchangeOfferWBRequest) (lib.ExchangeOfferWBResponse, error) {
	var offerWBRes lib.ExchangeOfferWBResponse
	var requestAsset string
	var requestAmount int64
	var err error

	request := offerRequest.Request
	if len(request) != 1 {
		err = fmt.Errorf("request must be a single record but has:%d", len(request))
		logger.Println("error:", err)
		return offerWBRes, err
	}
	for k, v := range request {
		requestAsset = k
		requestAmount = v
	}

	offer := offerRequest.Offer
	commitments := offerRequest.Commitments

	// 1. lookup rate
	tmp, err := lookupRate(requestAsset, requestAmount, offer)
	if err != nil {
		logger.Println("error:", err)
		return offerWBRes, err
	}

	offerWBRes.Fee = tmp.Fee
	offerWBRes.AssetLabel = tmp.AssetLabel
	offerWBRes.Cost = tmp.Cost

	// 2. lookup unspent
	utxos, err := rpcClient.SearchUnspent(lockList, requestAsset, requestAmount, true)
	if err != nil {
		logger.Println("error:", err)
		return offerWBRes, err
	}
	rautxos, err := rpcClient.SearchMinimalUnspent(lockList, offer, true)
	if err != nil {
		logger.Println("error:", err)
		return offerWBRes, err
	}

	// 3. creat tx
	tx, err := createTransactionTemplateWB(requestAsset, requestAmount, offer, offerWBRes, utxos, rautxos)
	if err != nil {
		logger.Println("error:", err)
		return offerWBRes, err
	}

	// 4. blinding
	cmutxos := append(utxos, rautxos...)
	resCommitments, err := rpcClient.GetCommitments(cmutxos)
	if err != nil {
		logger.Println("error:", err)
		return offerWBRes, err
	}
	commitments = append(resCommitments, commitments...)

	blindtx, _, err := rpcClient.RequestAndCastString("blindrawtransaction", tx, true, commitments)
	if err != nil {
		logger.Println("RPC/blindrawtransaction error:", err, tx)
		return offerWBRes, err
	}

	offerWBRes.Transaction = blindtx
	offerWBRes.Commitments = resCommitments

	return offerWBRes, nil
}

func doOffer(offerRequest lib.ExchangeOfferRequest) (lib.ExchangeOfferResponse, error) {
	var offerRes lib.ExchangeOfferResponse
	var requestAsset string
	var requestAmount int64
	var err error

	request := offerRequest.Request
	if len(request) != 1 {
		err = fmt.Errorf("request must be a single record but has:%d", len(request))
		logger.Println("error:", err)
		return offerRes, err
	}
	for k, v := range request {
		requestAsset = k
		requestAmount = v
	}

	offer := offerRequest.Offer

	// 1. lookup config
	tmp, err := lookupRate(requestAsset, requestAmount, offer)
	if err != nil {
		logger.Println("error:", err)
		return offerRes, err
	}

	offerRes.Fee = tmp.Fee
	offerRes.AssetLabel = tmp.AssetLabel
	offerRes.Cost = tmp.Cost

	// 2. lookup unspent
	utxos, err := rpcClient.SearchUnspent(lockList, requestAsset, requestAmount, false)
	if err != nil {
		logger.Println("error:", err)
		return offerRes, err
	}

	// 3. creat tx
	offerRes.Transaction, err = createTransactionTemplate(requestAsset, requestAmount, offer, offerRes.Cost, utxos)
	if err != nil {
		logger.Println("error:", err)
	}

	return offerRes, err
}

func createTransactionTemplate(requestAsset string, requestAmount int64, offer string, cost int64, utxos rpc.UnspentList) (string, error) {
	var addrOffer string
	var addrChange string
	var err error

	change := utxos.GetAmount() - requestAmount

	addrOffer, err = rpcClient.GetNewAddr(false)
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
		addrChange, err = rpcClient.GetNewAddr(false)
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

func createTransactionTemplateWB(requestAsset string, requestAmount int64, offer string, offerRes lib.ExchangeOfferWBResponse, utxos rpc.UnspentList, loopbackUtxos rpc.UnspentList) (string, error) {
	var addrOffer string
	var addrChange string
	var err error

	change := utxos.GetAmount() - requestAmount
	lbChange := loopbackUtxos.GetAmount() + offerRes.Cost

	addrOffer, err = rpcClient.GetNewAddr(true)
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
		addrChange, err = rpcClient.GetNewAddr(true)
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

func doSubmit(submitRequest lib.SubmitExchangeRequest) (lib.SubmitExchangeResponse, error) {
	var submitRes lib.SubmitExchangeResponse
	var rawTx rpc.RawTransaction
	var signedtx rpc.SignedTransaction
	var err error

	rcvtx := submitRequest.Transaction

	_, err = rpcClient.RequestAndUnmarshalResult(&rawTx, "decoderawtransaction", rcvtx)
	if err != nil {
		logger.Println("RPC/decoderawtransaction error:", err, rcvtx)
		return submitRes, err
	}

	// TODO check rawTx (consistency with offer etc...)

	_, err = rpcClient.RequestAndUnmarshalResult(&signedtx, "signrawtransaction", rcvtx)
	if err != nil {
		logger.Println("RPC/signrawtransaction error:", err, rcvtx)
		return submitRes, err
	}

	txid, _, err := rpcClient.RequestAndCastString("sendrawtransaction", signedtx.Hex)
	if err != nil {
		logger.Println("RPC/sendrawtransaction error:", err, signedtx.Hex)
		return submitRes, err
	}

	submitRes.TransactionID = txid

	for _, v := range rawTx.Vin {
		lockList.Unlock(v.Txid, v.Vout)
	}

	return submitRes, nil
}

func lookupRate(requestAsset string, requestAmount int64, offer string) (lib.ExchangeRateResponse, error) {
	var rateRes lib.ExchangeRateResponse

	rateMap, ok := fixedRateTable[offer]
	if !ok {
		err := fmt.Errorf("no exchange source:%s", offer)
		logger.Println("error:", err)
		return rateRes, err
	}

	rate, ok := rateMap[requestAsset]
	if !ok {
		err := fmt.Errorf("cannot exchange to:%s", requestAsset)
		logger.Println("error:", err)
		return rateRes, err
	}

	cost := int64(float64(requestAmount) / rate.Rate)
	if cost < rate.Min {
		err := fmt.Errorf("cost lower than min value:%d", cost)
		logger.Println("error:", err)
		return rateRes, err
	}
	if rate.Max < cost {
		err := fmt.Errorf("cost higher than max value:%d", cost)
		logger.Println("error:", err)
		return rateRes, err
	}

	rateRes = lib.ExchangeRateResponse{
		Fee:        rate.Fee,
		AssetLabel: offer,
		Cost:       cost,
	}

	return rateRes, nil
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

	dir, err := os.Getwd()
	if err != nil {
		logger.Println("error:", err)
		return
	}
	listener, err := lib.StartHTTPServer(localAddr, handlerList, dir+"/html/"+myActorName)
	if err != nil {
		logger.Println("error:", err)
		return
	}
	defer func() {
		e := listener.Close()
		if e != nil {
			logger.Println("error:", e)
		}
	}()

	_, err = lib.StartCyclic(lockList.Sweep, 3, true)
	if err != nil {
		logger.Println("error:", err)
		return
	}

	logger.Println(myActorName + " stop")
}
