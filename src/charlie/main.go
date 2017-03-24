// Copyright (c) 2017 DG Lab
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package main

import (
	"democonf"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"rpc"
	"sort"
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

type OfferResponse struct {
	Fee         int64  `json:"fee,string"`
	AssetLabel  string `json:"assetid"`
	Cost        int64  `json:"cost,string"`
	Transaction string `json:"tx"`
}

type SubmitResponse struct {
	TransactionId string `json:"txid"`
}

type ErrorResponse struct {
	Result  bool   `json:"result"`
	Message string `json:"message"`
}

type LockList map[string]time.Time

func (ll LockList) lock(txid string, vout int64) bool {
	key := getLockingKey(txid, vout)
	now := time.Now()
	to := now.Add(utxoLockDuration)

	old, ok := ll[key]
	if !ok {
		// new lock.
		ll[key] = to
		return true
	}
	if old.Sub(now) < 0 {
		// exists but no longer locked. lock again.
		ll[key] = to
		return true
	}

	// already locked.
	return false
}

func (ll LockList) unlock(txid string, vout int64) {
	key := getLockingKey(txid, vout)
	delete(ll, key)

	return
}

func (ll LockList) sweep() {
	now := time.Now()
	for k, v := range ll {
		if v.Sub(now) < 0 {
			delete(ll, k)
		}
	}
}

type UnspentList []*rpc.Unspent

func (ul UnspentList) Len() int {
	return len(ul)
}

func (ul UnspentList) Swap(i, j int) {
	ul[i], ul[j] = ul[j], ul[i]
}

func (ul UnspentList) Less(i, j int) bool {
	if (*ul[i]).Amount < (*ul[j]).Amount {
		return true
	}
	if (*ul[i]).Amount > (*ul[j]).Amount {
		return false
	}
	return (*ul[i]).Confirmations < (*ul[j]).Confirmations
}

func unlockUnspentList(ul UnspentList) {
	for _, u := range ul {
		lockList.unlock(u.Txid, u.Vout)
	}
}

type CyclicProcess struct {
	handler  func()
	interval int
}

var logger *log.Logger = log.New(os.Stdout, myActorName+":", log.LstdFlags+log.Lshortfile)
var conf = democonf.NewDemoConf(myActorName)
var stop bool = false
var assetIdMap = make(map[string]string)
var lockList = make(LockList)
var utxoLockDuration time.Duration
var rpcClient *rpc.Rpc
var elementsTxCommand string
var elementsTxOption string
var localAddr string
var defaultRateTuple ExchangeRateTuple = ExchangeRateTuple{Rate: 0.5, Min: 100, Max: 200000, Unit: 20, Fee: 15}
var fixedRateTable = make(map[string](map[string]ExchangeRateTuple))

var handlerList = map[string]func(url.Values, string) ([]byte, error){
	"/getexchangeoffer/": doOffer,
	"/submitexchange/":   doSubmit,
}

var cyclics = []CyclicProcess{CyclicProcess{handler: sweep, interval: 3}}

const (
	myActorName     = "charlie"
	defaultRateFrom = "AKISKY"
	defaultRateTo   = "MELON"
	defaultRpcURL   = "http://127.0.0.1:10020"
	defaultRpcUser  = "user"
	defaultPpcPass  = "pass"
	defaultListen   = "8020"
	defaultTxPath   = "elements-tx"
	defaultTxOption = ""
	defaultTimeout  = 600
)

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
		err = errors.New("type of offer is not string:" + fmt.Sprintf("%s", tmp))
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
	utxos, err := searchUnspent(requestAsset, requestAmount)
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

func createTransactionTemplate(requestAsset string, requestAmount int64, offer string, cost int64, utxos UnspentList) (string, error) {
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
		txin := "in=" + u.Txid + ":" + strconv.FormatInt(u.Vout, 10) + ":" + strconv.FormatInt(u.Amount, 10)
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
		err = errors.New("type of tx is not string:" + fmt.Sprintf("%V", tmp))
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

	submitRes := SubmitResponse{
		TransactionId: txid,
	}

	for _, v := range rawTx.Vin {
		lockList.unlock(v.Txid, v.Vout)
	}

	b, _ := json.Marshal(submitRes)
	logger.Println("<<" + string(b))
	return b, nil
}

func lookupRate(requestAsset string, requestAmount int64, offer string) (OfferResponse, error) {
	var offerRes OfferResponse

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

	offerRes = OfferResponse{
		Fee:         rate.Fee,
		Transaction: "",
		AssetLabel:  offer,
		Cost:        cost,
	}

	return offerRes, nil
}

func searchUnspent(requestAsset string, requestAmount int64) (UnspentList, error) {
	var totalAmount int64 = 0
	var ul UnspentList
	var utxos UnspentList = make(UnspentList, 0)

	_, err := rpcClient.RequestAndUnmarshalResult(&ul, "listunspent", 1, 9999999, []string{}, requestAsset)
	if err != nil {
		logger.Println("RPC/listunspent error:", err, fmt.Sprintf("\n\tparam :%#v", requestAsset))
		return utxos, err
	}
	sort.Sort(sort.Reverse(ul))

	for _, u := range ul {
		if requestAmount < totalAmount {
			break
		}
		if !lockList.lock(u.Txid, u.Vout) {
			continue
		}
		if !(u.Spendable || u.Solvable) {
			continue
		}
		totalAmount += u.Amount
		utxos = append(utxos, u)
	}

	if requestAmount >= totalAmount {
		unlockUnspentList(utxos)
		err = errors.New("no sufficient utxo.")
		logger.Println("error:", err)
		return utxos, err
	}

	return utxos, nil
}

func getLockingKey(txid string, vout int64) string {
	return fmt.Sprintf("%s:%d", txid, vout)
}

func getAmount(ul UnspentList) int64 {
	var totalAmount int64 = 0

	for _, u := range ul {
		totalAmount += u.Amount
	}

	return totalAmount
}

func sweep() {
	lockList.sweep()
}

func initialize() {
	logger = log.New(os.Stdout, myActorName+":", log.LstdFlags+log.Lshortfile)

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
	utxoLockDuration = time.Duration(int64(conf.GetNumber("timeout", defaultTimeout))) * time.Second
	fixedRateTable[defaultRateFrom] = map[string]ExchangeRateTuple{defaultRateTo: defaultRateTuple}
	conf.GetInterface("fixrate", &fixedRateTable)
}

func stratHttpServer(laddr string, handlers map[string]func(url.Values, string) ([]byte, error), filepath string) (net.Listener, error) {
	listener, err := net.Listen("tcp", laddr)
	if err != nil {
		logger.Println("net#Listen error:", err)
		return listener, err
	}

	mux := http.NewServeMux()
	for p, h := range handlers {
		f := generateMuxHandler(h)
		mux.HandleFunc(p, f)
	}

	mux.Handle("/", http.FileServer(http.Dir(filepath)))
	logger.Println("start listen...", listener.Addr().Network(), listener.Addr())
	go http.Serve(listener, mux)

	return listener, err
}

func generateMuxHandler(h func(url.Values, string) ([]byte, error)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		handler(w, r, h)
		return
	}
}

func handler(w http.ResponseWriter, r *http.Request, f func(url.Values, string) ([]byte, error)) {
	w.Header().Add("Access-Control-Allow-Origin", "*")
	w.Header().Add("Access-Control-Allow-Methods", "GET")
	w.Header().Add("Access-Control-Allow-Headers", r.Header.Get("Access-Control-Request-Headers"))
	w.Header().Add("Access-Control-Max-Age", "-1")

	status := http.StatusOK
	var res []byte
	var err error
	defer r.Body.Close()

	switch r.Method {
	case "GET":
		r.ParseForm()
		res, err = f(r.Form, "")

	case "POST":
		req, err := ioutil.ReadAll(r.Body)
		defer r.Body.Close()
		if err != nil {
			status = http.StatusInternalServerError
			logger.Println("ioutil#ReadAll error:", err)
			_, _ = w.Write(nil)
			return
		}
		res, err = f(nil, string(req))

	default:
		status = http.StatusMethodNotAllowed
	}

	if err != nil {
		logger.Println("error:", err)
		status = http.StatusInternalServerError
		if res != nil {
			res = createErrorByteArray(err)
		}
	}

	w.WriteHeader(status)
	_, err = w.Write(res)
	if err != nil {
		logger.Println("w#Write Error:", err)
		return
	}
}

func createErrorByteArray(e error) []byte {
	if e == nil {
		e = errors.New("error occured.(fake)")
	}
	res := ErrorResponse{
		Result:  false,
		Message: fmt.Sprintf("%s", e),
	}
	b, _ := json.Marshal(res)
	return b
}

func cyclicProcStart(cps []CyclicProcess) {
	for _, cyclic := range cps {
		go func() {
			fmt.Println("Loop interval:", cyclic.interval)
			for {
				time.Sleep(time.Duration(cyclic.interval) * time.Second)
				if stop {
					break
				}
				cyclic.handler()
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
	listener, err := stratHttpServer(localAddr, handlerList, dir+"/html/"+myActorName)
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
