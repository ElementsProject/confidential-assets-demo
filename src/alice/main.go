// Copyright (c) 2017 DG Lab
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

package main

import (
	"bytes"
	"crypto/sha256"
	"democonf"
	"encoding/binary"
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

type UserOfferRequest struct {
	Offer string `json:"offer"`
	Cost  int64  `json:"cost"`
}
type UserOfferResByAsset struct {
	Fee         int64  `json:"fee"`
	Cost        int64  `json:"cost"`
	ID          string `json:"id"`
	Transaction string `json:"-"`
}

type UserOfferResponse map[string]UserOfferResByAsset

type UserSendResponse struct {
	Result  bool   `json:"result"`
	Message string `json:"message"`
}

type UserWalletInfoRes struct {
	Balance rpc.Balance `json:"balance"`
}

type ErrorResponse struct {
	Result  bool   `json:"result"`
	Message string `json:"message"`
}

type ExchangeOfferRequest struct {
	Request map[string]int64 `json:"request"`
	Offer   string           `json:"offer"`
}

type ExchangeOfferResponse struct {
	Fee         int64  `json:"fee,string"`
	AssetLabel  string `json:"assetid"`
	Cost        int64  `json:"cost,string"`
	Transaction string `json:"tx"`
}

func (u *ExchangeOfferResponse) getID() string {
	tx := u.Transaction
	now := time.Now().Unix()
	nowba := make([]byte, binary.MaxVarintLen64)
	binary.PutVarint(nowba, now)
	target := append([]byte(tx), nowba...)
	hash := sha256.Sum256(target)
	id := fmt.Sprintf("%x", hash)
	return id
}

type SubmitExchangeRequest struct {
	Transaction string `json:"tx"`
}

type SubmitExchangeResponse struct {
	TransactionId string `json:"txid"`
}

type Quotation struct {
	RequestAsset  string
	RequestAmount int64
	Offer         map[string]UserOfferResByAsset
}

func (e *Quotation) getID() string {
	now := time.Now().Unix()
	target := make([]byte, binary.MaxVarintLen64)
	binary.PutVarint(target, now)
	for _, v := range e.Offer {
		if v.ID == "" {
			continue
		}
		target = append(target, []byte(v.ID)...)
	}
	hash := sha256.Sum256(target)
	id := fmt.Sprintf("%x", hash)
	return id
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
var quotationList = make(map[string]Quotation)
var exchangerConf = democonf.NewDemoConf(exchangerName)
var exchangeOfferURL string
var exchangeSubmitURL string
var confidential = false

var handlerList = map[string]func(url.Values, string) ([]byte, error){
	"/walletinfo": doWalletInfo,
	"/offer":      doOffer,
	"/send":       doSend,
}

var cyclics = []CyclicProcess{CyclicProcess{handler: sweep, interval: 3}}

const (
	myActorName          = "alice"
	defaultRpcURL        = "http://127.0.0.1:10000"
	defaultRpcUser       = "user"
	defaultPpcPass       = "pass"
	defaultListen        = "8000"
	defaultTxPath        = "elements-tx"
	defaultTxOption      = ""
	defaultTimeout       = 600
	exchangerName        = "charlie"
	defaultCharlieListen = "8020"
)

func getMyBalance() (rpc.Balance, error) {
	wallet, err := getWalletInfo()
	if err != nil {
		logger.Println("error:", err)
		return nil, err
	}
	chooseKnownAssets(wallet.Balance)
	if err != nil {
		logger.Println("error:", err)
		return nil, err
	}
	return wallet.Balance, nil
}

func doWalletInfo(reqParam url.Values, reqBody string) ([]byte, error) {
	logger.Println(fmt.Sprintf("doWalletInfo start. %#v", reqParam))
	balance, err := getMyBalance()
	if err != nil {
		logger.Println("error:", err)
		return nil, err
	}

	walletInfoRes := UserWalletInfoRes{
		Balance: balance,
	}

	b, err := json.Marshal(walletInfoRes)
	if err != nil {
		logger.Println("json#Marshal error:", err)
		return nil, err
	}

	logger.Println("<<" + string(b))
	return b, nil

}
func chooseKnownAssets(b rpc.Balance) {
	for k, _ := range b {
		if _, ok := assetIdMap[k]; !ok {
			delete(b, k)
		}
	}
	return
}

func doOffer(reqParam url.Values, reqBody string) ([]byte, error) {
	logger.Println(fmt.Sprintf("doOffer start. %#v", reqParam))
	userOfferResponse := make(UserOfferResponse)
	var quotation Quotation

	tmp, ok := reqParam["asset"]
	fmt.Printf("%#v¥n\n", reqParam)
	if !ok {
		err := errors.New("no offer asset label found:")
		logger.Println("error:", err)
		return nil, err
	}

	if len(tmp) != 1 {
		err := errors.New("offer must single record:" + fmt.Sprintf("%d", len(tmp)))
		logger.Println("error:", err)
		return nil, err
	}
	requestAsset := tmp[0]

	tmp, ok = reqParam["cost"]
	if !ok {
		err := errors.New("no offer asset amount found:")
		logger.Println("error:", err)
		return nil, err
	}
	if len(tmp) != 1 {
		err := errors.New("cost must single record:" + fmt.Sprintf("%d", len(tmp)))
		logger.Println("error:", err)
		return nil, err
	}
	requestAmount, err := strconv.ParseInt(tmp[0], 10, 64)
	if err != nil {
		logger.Println("error:", err)
		return nil, err
	}

	balance, err := getMyBalance()
	if err != nil {
		logger.Println("error:", err)
		return nil, err
	}
	quotation.RequestAsset = requestAsset
	quotation.RequestAmount = requestAmount
	quotation.Offer = make(map[string]UserOfferResByAsset)
	offerExists := false
	for offerAsset, _ := range balance {
		if offerAsset == requestAsset {
			continue
		}
		exchangeOffer, err := getexchangeoffer(requestAsset, requestAmount, offerAsset)
		if err != nil {
			continue
		}
		offerExists = true
		offerByAsset := UserOfferResByAsset{
			Fee:         exchangeOffer.Fee,
			Cost:        exchangeOffer.Cost,
			ID:          exchangeOffer.getID(),
			Transaction: exchangeOffer.Transaction,
		}
		userOfferResponse[offerAsset] = offerByAsset
		quotation.Offer[offerAsset] = offerByAsset
	}
	if offerExists {
		quotationList[quotation.getID()] = quotation
	}
	b, err := json.Marshal(userOfferResponse)
	if err != nil {
		logger.Println("json#Marshal error:", err)
		return nil, err
	}

	logger.Println("<<" + string(b))
	return b, nil
}

func appendTransactionInfo(sendToAddr string, sendAsset string, sendAmount int64, offerAsset string, offerDetail UserOfferResByAsset, utxos UnspentList) (string, error) {
	template := offerDetail.Transaction
	cost := offerDetail.Cost
	fee := offerDetail.Fee
	change := getAmount(utxos) - (cost + fee)
	params := []string{}

	if elementsTxOption != "" {
		params = append(params, elementsTxOption)
	}
	params = append(params, template)

	for _, u := range utxos {
		txin := "in=" + u.Txid + ":" + strconv.FormatInt(u.Vout, 10) + ":" + strconv.FormatInt(u.Amount, 10)
		params = append(params, txin)
	}

	if 0 < change {
		addrChange, err := rpcClient.GetNewAddr(false)
		if err != nil {
			return "", err
		}
		outAddrChange := "outaddr=" + strconv.FormatInt(change, 10) + ":" + addrChange + ":" + assetIdMap[offerAsset]
		params = append(params, outAddrChange)
	}
	outAddrSend := "outaddr=" + strconv.FormatInt(sendAmount, 10) + ":" + sendToAddr + ":" + assetIdMap[sendAsset]
	outAddrFee := "outscript=" + strconv.FormatInt(fee, 10) + "::" + assetIdMap[offerAsset]
	params = append(params, outAddrSend, outAddrFee)

	out, err := exec.Command(elementsTxCommand, params...).Output()

	if err != nil {
		logger.Println("elements-tx error:", err, fmt.Sprintf("\n\tparams: %#v\n\toutput: %#v", params, out))
		return "", err
	}

	txTemplate := strings.TrimRight(string(out), "\n")
	return txTemplate, nil
}

func doSend(reqParam url.Values, reqBody string) ([]byte, error) {
	logger.Println(fmt.Sprintf("doSend start. %#v", reqParam))
	tmp, ok := reqParam["id"]
	if !ok {
		err := errors.New("no id found:")
		logger.Println("error:", err)
		return nil, err
	}

	if len(tmp) != 1 {
		err := errors.New("id must single record:" + fmt.Sprintf("%d", len(tmp)))
		logger.Println("error:", err)
		return nil, err
	}
	offerID := tmp[0]

	tmp, ok = reqParam["addr"]
	if !ok {
		err := errors.New("no addr found:")
		logger.Println("error:", err)
		return nil, err
	}

	if len(tmp) != 1 {
		err := errors.New("addr must single record:" + fmt.Sprintf("%d", len(tmp)))
		logger.Println("error:", err)
		return nil, err
	}
	sendToAddr := tmp[0]

	estID := ""
	found := false

	var sendAsset string
	var sendAmount int64
	var offerAsset string
	var offerDetail UserOfferResByAsset
	for i, v := range quotationList {
		for k, w := range v.Offer {
			if w.ID == offerID {
				offerAsset = k
				offerDetail = w
				found = true
				break
			}
		}
		if found {
			sendAsset = v.RequestAsset
			sendAmount = v.RequestAmount
			estID = i
			break
		}
	}

	utxos, err := searchUnspent(offerAsset, offerDetail.Cost+offerDetail.Fee)
	if err != nil {
		logger.Println("error:", err)
		return nil, err
	}

	tx, err := appendTransactionInfo(sendToAddr, sendAsset, sendAmount, offerAsset, offerDetail, utxos)
	if err != nil {
		logger.Println("error:", err)
		return nil, err
	}

	var signedtx rpc.SignedTransaction
	_, err = rpcClient.RequestAndUnmarshalResult(&signedtx, "signrawtransaction", tx)
	if err != nil {
		logger.Println("RPC/signrawtransaction error:", err, fmt.Sprintf("\n\tparam :%#v", tx))
		return nil, err
	}

	var userSendResponse UserSendResponse
	submitRes, err := submitexchange(signedtx.Hex)
	if err != nil {
		userSendResponse.Result = false
		userSendResponse.Message = fmt.Sprintf("fail ADDR:%s TxID:%s\nerr:%#v", sendToAddr, offerID, err)
	}
	userSendResponse.Result = true
	userSendResponse.Message = fmt.Sprintf("success ADDR:%s TxID:%s ", sendToAddr, submitRes.TransactionId)

	delete(quotationList, estID)

	unlockUnspentList(utxos)

	b, _ := json.Marshal(userSendResponse)
	logger.Println("<<" + string(b))
	return b, err
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

func getWalletInfo() (rpc.Wallet, error) {
	var walletInfo rpc.Wallet

	_, err := rpcClient.RequestAndUnmarshalResult(&walletInfo, "getwalletinfo")
	if err != nil {
		logger.Println("RPC/getwalletinfo error:", err)
		return walletInfo, err
	}

	return walletInfo, nil
}

func getexchangeoffer(requestAsset string, requestAmount int64, offerAsset string) (ExchangeOfferResponse, error) {
	var offerRes ExchangeOfferResponse
	var offerReq ExchangeOfferRequest
	offerReq.Request = make(map[string]int64)
	offerReq.Request[requestAsset] = requestAmount
	offerReq.Offer = offerAsset

	_, err := callExchangerAPI(exchangeOfferURL, offerReq, &offerRes)

	if err != nil {
		logger.Println("json#Marshal error:", err)
	}
	return offerRes, err
}

func submitexchange(tx string) (SubmitExchangeResponse, error) {
	var submitReq SubmitExchangeRequest
	var submitRes SubmitExchangeResponse
	submitReq.Transaction = tx

	_, err := callExchangerAPI(exchangeSubmitURL, submitReq, &submitRes)

	if err != nil {
		logger.Println("json#Marshal error:", err)
	}
	return submitRes, err
}

func callExchangerAPI(targetUrl string, param interface{}, result interface{}) (*http.Response, error) {
	encoded_request, err := json.Marshal(param)
	if err != nil {
		logger.Println("json#Marshal error:", err)
		return nil, err
	}
	client := &http.Client{}
	reqBody := string(encoded_request)
	req, err := http.NewRequest("POST", targetUrl, bytes.NewBufferString(reqBody))
	fmt.Println(req)
	if err != nil {
		logger.Println("http#NewRequest error", err)
		return nil, err
	}
	res, err := client.Do(req)
	fmt.Println(res)
	if err != nil {
		logger.Println("http.Client#Do error:", err)
		return nil, err
	}
	body, err := ioutil.ReadAll(res.Body)
	defer res.Body.Close()
	if err != nil {
		logger.Println("ioutil#ReadAll error:", err)
		return res, err
	}
	err = json.Unmarshal(body, result)
	if err != nil {
		return res, err
	}
	return res, nil
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

	exchangeOfferURL = "http://127.0.0.1" + exchangerConf.GetString("laddr", defaultCharlieListen) + "/getexchangeoffer/"
	exchangeSubmitURL = "http://127.0.0.1" + exchangerConf.GetString("laddr", defaultCharlieListen) + "/submitexchange/"
	//	exchangeOfferURL = exchangerConf.GetString("laddr", defaultCharlieListen) + "/getexchangeoffer/"
	//	exchangeSubmitURL = exchangerConf.GetString("laddr", defaultCharlieListen) + "/submitexchange/"
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
