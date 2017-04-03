// Copyright (c) 2017 DG Lab
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package main

import (
	"democonf"
	"encoding/json"
	"errors"
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

type ExchangeRateTuple struct {
	Rate float64 `json:"rate"`
	Min  int64   `json:"min"`
	Max  int64   `json:"max"`
	Unit int64   `json:"unit"`
	Fee  int64   `json:"fee,"`
}

const (
	myActorName     = "charlie"
	defaultRateFrom = "AKISKY"
	defaultRateTo   = "MELON"
	defaultRpcURL   = "http://127.0.0.1:10020"
	defaultRpcUser  = "user"
	defaultPpcPass  = "pass"
	defaultListen   = ":8020"
	defaultTxPath   = "elements-tx"
	defaultTxOption = ""
	defaultTimeout  = 600
)

var logger *log.Logger = log.New(os.Stdout, myActorName+":", log.LstdFlags+log.Lshortfile)
var conf = democonf.NewDemoConf(myActorName)
var stop bool = false
var assetIdMap = make(map[string]string)
var lockList = make(rpc.LockList)
var rpcClient *rpc.Rpc
var elementsTxCommand string
var elementsTxOption string
var localAddr string
var defaultRateTuple ExchangeRateTuple = ExchangeRateTuple{Rate: 0.5, Min: 100, Max: 200000, Unit: 20, Fee: 15}
var fixedRateTable = make(map[string](map[string]ExchangeRateTuple))

var handlerList = map[string]func(url.Values, string) ([]byte, error){
	"/getexchangerate/":    doGetRate,
	"/getexchangeofferwb/": doOfferWithBlinding,
	"/getexchangeoffer/":   doOffer,
	"/submitexchange/":     doSubmit,
}

var cyclics = []lib.CyclicProcess{lib.CyclicProcess{Handler: lockList.Sweep, Interval: 3}}

func getReqestBodyMap(reqBody string) (map[string]interface{}, error) {
	var reqBodyMap interface{}

	err := json.Unmarshal([]byte(reqBody), &reqBodyMap)
	if err != nil {
		logger.Println("json#Unmarshal error:", err)
		return nil, err
	}

	switch reqBodyMap.(type) {
	case map[string]interface{}:
		return reqBodyMap.(map[string]interface{}), nil
	default:
		err = errors.New("JSON type missmatch:" + fmt.Sprintf("%V", reqBodyMap))
		logger.Println("error:", err)
	}
	return nil, err
}

func doGetRate(reqParam url.Values, reqBody string) ([]byte, error) {
	var requestAsset string
	var requestAmount int64

	rateReqMap, err := getReqestBodyMap(reqBody)
	if err != nil {
		logger.Println("error:", err)
		return nil, err
	}

	tmp, ok := rateReqMap["request"]
	request := tmp.(map[string]interface{})
	if len(request) != 1 {
		err = errors.New("request must single record:" + fmt.Sprintf("%d", len(request)))
		logger.Println("error:", err)
		return nil, err
	}
	for k, v := range request {
		requestAsset = k
		requestAmount = int64(v.(float64))
	}

	tmp, ok = rateReqMap["offer"]
	if !ok {
		err = errors.New("offer not found.")
		logger.Println("error:", err)
		b, _ := json.Marshal(fmt.Sprintf("%v", err))
		return b, nil
	}

	offer, ok := tmp.(string)
	if !ok {
		err = errors.New("type of offer is not a string:" + fmt.Sprintf("%s", tmp))
		logger.Println("error:", err)
		return nil, err
	}

	// 1. lookup config
	rateRes, err := lookupRate(requestAsset, requestAmount, offer)
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
		err = errors.New("request must single record:" + fmt.Sprintf("%d", len(request)))
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
		logger.Println("RPC/blindrawtransaction error:", err, fmt.Sprintf("\n\tparam :%#v", tx))
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

	offerReqMap, err := getReqestBodyMap(reqBody)
	if err != nil {
		logger.Println("error:", err)
		return nil, err
	}

	tmp, ok := offerReqMap["request"]
	request := tmp.(map[string]interface{})
	if len(request) != 1 {
		err = errors.New("request must single record:" + fmt.Sprintf("%d", len(request)))
		logger.Println("error:", err)
		return nil, err
	}
	for k, v := range request {
		requestAsset = k
		requestAmount = int64(v.(float64))
	}

	tmp, ok = offerReqMap["offer"]
	if !ok {
		err = errors.New("offer not found.")
		logger.Println("error:", err)
		b, _ := json.Marshal(fmt.Sprintf("%v", err))
		return b, nil
	}

	offer, ok := tmp.(string)
	if !ok {
		err = errors.New("type of offer is not a string:" + fmt.Sprintf("%s", tmp))
		logger.Println("error:", err)
		return nil, err
	}

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
	change := getAmount(utxos) - requestAmount

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
		//		txin := "in=" + u.Txid + ":" + strconv.FormatInt(u.Vout, 10) + ":" + strconv.FormatInt(u.Amount, 10)
		txin := "in=" + u.Txid + ":" + strconv.FormatInt(u.Vout, 10)
		params = append(params, txin)
	}

	outAddrOffer := "outaddr=" + strconv.FormatInt(cost, 10) + ":" + addrOffer + ":" + assetIdMap[offer]
	params = append(params, outAddrOffer)

	if 0 < change {
		addrChange, err := rpcClient.GetNewAddr(false)
		if err != nil {
			return "", err
		}
		outAddrChange := "outaddr=" + strconv.FormatInt(change, 10) + ":" + addrChange + ":" + assetIdMap[requestAsset]
		params = append(params, outAddrChange)
	}

	out, err := exec.Command(elementsTxCommand, params...).Output()

	if err != nil {
		logger.Println("elements-tx error:", err, fmt.Sprintf("\n\tparams: %#v\n\toutput: %#v", params, out))
		return "", err
	}

	txTemplate := strings.TrimRight(string(out), "\n")
	return txTemplate, nil
}

func createTransactionTemplateWB(requestAsset string, requestAmount int64, offer string, offerRes lib.ExchangeOfferResponse, utxos rpc.UnspentList, loopbackUtxos rpc.UnspentList) (string, error) {
	change := getAmount(utxos) - requestAmount
	lbChange := getAmount(loopbackUtxos) + offerRes.Cost

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
		//		txin := "in=" + u.Txid + ":" + strconv.FormatInt(u.Vout, 10) + ":" + strconv.FormatInt(u.Amount, 10)
		txin := "in=" + u.Txid + ":" + strconv.FormatInt(u.Vout, 10)
		params = append(params, txin)
	}
	for _, u := range loopbackUtxos {
		//		txin := "in=" + u.Txid + ":" + strconv.FormatInt(u.Vout, 10) + ":" + strconv.FormatInt(u.Amount, 10)
		txin := "in=" + u.Txid + ":" + strconv.FormatInt(u.Vout, 10)
		params = append(params, txin)
	}

	outAddrOffer := "outaddr=" + strconv.FormatInt(lbChange, 10) + ":" + addrOffer + ":" + assetIdMap[offer]
	params = append(params, outAddrOffer)

	if 0 < change {
		addrChange, err := rpcClient.GetNewAddr(true)
		if err != nil {
			return "", err
		}
		outAddrChange := "outaddr=" + strconv.FormatInt(change, 10) + ":" + addrChange + ":" + assetIdMap[requestAsset]
		params = append(params, outAddrChange)
	}
	outAddrFee := "outscript=" + strconv.FormatInt(offerRes.Fee, 10) + "::" + assetIdMap[offer]
	params = append(params, outAddrFee)

	logger.Println("=== tx-command param start ===")
	for _, p := range params {
		logger.Println(p)
	}
	logger.Println("=== tx-command param end ===")

	out, err := exec.Command(elementsTxCommand, params...).Output()

	if err != nil {
		logger.Println("elements-tx error:", err, fmt.Sprintf("\n\tparams: %#v\n\toutput: %#v", params, out))
		return "", err
	}

	txTemplate := strings.TrimRight(string(out), "\n")
	return txTemplate, nil
}

func doSubmit(rreqParam url.Values, reqBody string) ([]byte, error) {
	var rawTx rpc.RawTransaction
	var signedtx rpc.SignedTransaction

	submitReqMap, err := getReqestBodyMap(reqBody)
	if err != nil {
		return nil, err
	}

	tmp, ok := submitReqMap["tx"]
	if !ok {
		err = errors.New("no transaction:" + fmt.Sprintf("%V", submitReqMap))
		logger.Println("error:", err)
		return nil, err
	}

	rcvtx, ok := tmp.(string)
	if !ok {
		err = errors.New("type of tx is not a string:" + fmt.Sprintf("%V", tmp))
		logger.Println("error:", err)
		return nil, err
	}

	_, err = rpcClient.RequestAndUnmarshalResult(&rawTx, "decoderawtransaction", rcvtx)
	if err != nil {
		logger.Println("RPC/decoderawtransaction error:", err, fmt.Sprintf("\n\tparam :%#v", rcvtx))
		return nil, err
	}

	// TODO check rawTx (consistency with offer etc...)

	_, err = rpcClient.RequestAndUnmarshalResult(&signedtx, "signrawtransaction", rcvtx)
	if err != nil {
		logger.Println("RPC/signrawtransaction error:", err, fmt.Sprintf("\n\tparam :%#v", rcvtx))
		return nil, err
	}

	txid, _, err := rpcClient.RequestAndCastString("sendrawtransaction", signedtx.Hex)
	if err != nil {
		logger.Println("RPC/sendrawtransaction error:", err, fmt.Sprintf("\n\tparam :%#v", signedtx.Hex))
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
		err := errors.New("no exchange source:" + fmt.Sprintf("%s", offer))
		logger.Println("error:", err)
		return offerRes, err
	}

	rate, ok := rateMap[requestAsset]
	if !ok {
		err := errors.New("cannot exchange to:" + fmt.Sprintf("%s", requestAsset))
		logger.Println("error:", err)
		return offerRes, err
	}

	cost := int64(float64(requestAmount) / rate.Rate)
	if cost < rate.Min {
		err := errors.New("cost lower than min value:" + fmt.Sprintf("%d", cost))
		logger.Println("error:", err)
		return offerRes, err
	}
	if rate.Max < cost {
		err := errors.New("cost higher than max value:" + fmt.Sprintf("%d", cost))
		logger.Println("error:", err)
		return offerRes, err
	}

	offerRes = lib.ExchangeOfferResponse{
		Fee:         rate.Fee,
		Transaction: "",
		AssetLabel:  offer,
		Cost:        cost,
	}

	return offerRes, nil
}

func getAmount(ul rpc.UnspentList) int64 {
	var totalAmount int64 = 0

	for _, u := range ul {
		totalAmount += u.Amount
	}

	return totalAmount
}

func initialize() {
	logger = log.New(os.Stdout, myActorName+":", log.LstdFlags+log.Lshortfile)
	lib.SetLogger(logger)

	rpcClient = rpc.NewRpc(
		conf.GetString("rpcurl", defaultRpcURL),
		conf.GetString("rpcuser", defaultRpcUser),
		conf.GetString("rpcpass", defaultPpcPass))
	_, err := rpcClient.RequestAndUnmarshalResult(&assetIdMap, "dumpassetlabels")
	if err != nil {
		logger.Println("RPC/dumpassetlabels error:", err)
	}
	delete(assetIdMap, "bitcoin")

	localAddr = conf.GetString("laddr", defaultListen)
	elementsTxCommand = conf.GetString("txpath", defaultTxPath)
	elementsTxOption = conf.GetString("txoption", defaultTxOption)
	rpc.SetUtxoLockDuration(time.Duration(int64(conf.GetNumber("timeout", defaultTimeout))) * time.Second)
	fixedRateTable[defaultRateFrom] = map[string]ExchangeRateTuple{defaultRateTo: defaultRateTuple}
	conf.GetInterface("fixrate", &fixedRateTable)
}

func cyclicProcStart(cps []lib.CyclicProcess) {
	for _, cyclic := range cps {
		go func() {
			fmt.Println("Loop interval:", cyclic.Interval)
			for {
				time.Sleep(time.Duration(cyclic.Interval) * time.Second)
				if stop {
					break
				}
				cyclic.Handler()
			}
		}()
	}
}

func waitStopSignal() {
	for {
		time.Sleep(1 * time.Second)
		if stop {
			break
		}
	}
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
	signal.Notify(sig, syscall.SIGINT)
	go func() {
		logger.Println(<-sig)
		stop = true
	}()

	cyclicProcStart(cyclics)

	waitStopSignal()

	logger.Println(myActorName + " stop")
}
