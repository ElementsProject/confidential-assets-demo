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
	"lib"
	"log"
	"net/http"
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
	Balance rpc.BalanceMap `json:"balance"`
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

const (
	myActorName          = "alice"
	defaultRpcURL        = "http://127.0.0.1:10000"
	defaultRpcUser       = "user"
	defaultPpcPass       = "pass"
	defaultListen        = ":8000"
	defaultTxPath        = "elements-tx"
	defaultTxOption      = ""
	defaultTimeout       = 600
	exchangerName        = "charlie"
	defaultCharlieListen = ":8020"
)

var logger *log.Logger = log.New(os.Stdout, myActorName+":", log.LstdFlags+log.Lshortfile)
var conf = democonf.NewDemoConf(myActorName)
var stop bool = false
var assetIdMap = make(map[string]string)
var lockList = make(rpc.LockList)
var utxoLockDuration time.Duration
var rpcClient *rpc.Rpc
var elementsTxCommand string
var elementsTxOption string
var localAddr string
var quotationList = make(map[string]Quotation)
var exchangerConf = democonf.NewDemoConf(exchangerName)
var exchangeRateURL string
var exchangeOfferWBURL string
var exchangeOfferURL string
var exchangeSubmitURL string
var confidential = false

var handlerList = map[string]func(url.Values, string) ([]byte, error){
	"/walletinfo": doWalletInfo,
	"/offer":      doOffer,
	"/send":       doSend,
}

var cyclics = []lib.CyclicProcess{lib.CyclicProcess{Handler: lockList.Sweep, Interval: 3}}

func getMyBalance() (rpc.BalanceMap, error) {
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

func chooseKnownAssets(b rpc.BalanceMap) {
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
		err := errors.New("cost must be a single record:" + fmt.Sprintf("%d", len(tmp)))
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
		exchangeOffer, err := getexchangerate(requestAsset, requestAmount, offerAsset)
		if err != nil {
			continue
		}
		offerExists = true
		offerByAsset := UserOfferResByAsset{
			Fee:         exchangeOffer.Fee,
			Cost:        exchangeOffer.Cost,
			ID:          exchangeOffer.GetID(),
			Transaction: "",
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

func appendTransactionInfo(sendToAddr string, sendAsset string, sendAmount int64, offerAsset string, offerDetail UserOfferResByAsset, utxos rpc.UnspentList) (string, error) {
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
		//		txin := "in=" + u.Txid + ":" + strconv.FormatInt(u.Vout, 10) + ":" + strconv.FormatInt(u.Amount, 10)
		txin := "in=" + u.Txid + ":" + strconv.FormatInt(u.Vout, 10)
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

func appendTransactionInfoWB(sendToAddr string, sendAsset string, sendAmount int64, offerAsset string, offerDetail UserOfferResByAsset, utxos rpc.UnspentList, loopbackUtxos rpc.UnspentList) (string, error) {
	template := offerDetail.Transaction
	cost := offerDetail.Cost
	fee := offerDetail.Fee
	change := getAmount(utxos) - (cost + fee)
	params := []string{}
	lbChange := getAmount(loopbackUtxos)

	if elementsTxOption != "" {
		params = append(params, elementsTxOption)
	}
	params = append(params, template)

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

	if 0 < change {
		addrChange, err := rpcClient.GetNewAddr(true)
		if err != nil {
			return "", err
		}
		outAddrChange := "outaddr=" + strconv.FormatInt(change, 10) + ":" + addrChange + ":" + assetIdMap[offerAsset]
		params = append(params, outAddrChange)
	}
	if 0 < lbChange {
		addrLbChange, err := rpcClient.GetNewAddr(true)
		if err != nil {
			return "", err
		}
		outAddrLbChange := "outaddr=" + strconv.FormatInt(lbChange, 10) + ":" + addrLbChange + ":" + assetIdMap[sendAsset]
		params = append(params, outAddrLbChange)
	}
	outAddrSend := "outaddr=" + strconv.FormatInt(sendAmount, 10) + ":" + sendToAddr + ":" + assetIdMap[sendAsset]
	params = append(params, outAddrSend)

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

func doSend(reqParam url.Values, reqBody string) ([]byte, error) {
	tmp, ok := reqParam["id"]
	if !ok {
		err := errors.New("no id found:")
		logger.Println("error:", err)
		return nil, err
	}

	if len(tmp) != 1 {
		err := errors.New("id must be a single record:" + fmt.Sprintf("%d", len(tmp)))
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
		err := errors.New("addr must be a single record:" + fmt.Sprintf("%d", len(tmp)))
		logger.Println("error:", err)
		return nil, err
	}

	isConfidential, err := isConfidential(tmp[0])
	if err != nil {
		logger.Println("error:", err)
		return nil, err
	}

	sendToAddr := tmp[0]

	var userSendResponse UserSendResponse
	if isConfidential {
		userSendResponse, err = doSendWithBlinding(offerID, sendToAddr)
	} else {
		userSendResponse, err = doSendWithNoBlinding(offerID, sendToAddr)
	}

	b, _ := json.Marshal(userSendResponse)
	logger.Println("<<" + string(b))
	return b, err
}

func doSendWithBlinding(offerID string, sendToAddr string) (UserSendResponse, error) {
	logger.Println(fmt.Sprintf("doSendWithBlinding start. (%s, %s)", offerID, sendToAddr))

	var userSendResponse UserSendResponse

	quotationID, offerAsset, err := getQuotation(quotationList, offerID)
	if err != nil {
		logger.Println("error:", err)
		return userSendResponse, err
	}

	offerDetail := quotationList[quotationID].Offer[offerAsset]
	sendAsset := quotationList[quotationID].RequestAsset
	sendAmount := quotationList[quotationID].RequestAmount

	ofutxos, err := rpcClient.SearchUnspent(lockList, offerAsset, offerDetail.Cost+offerDetail.Fee, true)
	if err != nil {
		logger.Println("error:", err)
		return userSendResponse, err
	}
	sautxos, err := rpcClient.SearchMinimalUnspent(lockList, sendAsset, true)
	if err != nil {
		logger.Println("error:", err)
		return userSendResponse, err
	}

	cmutxos := append(ofutxos, sautxos...)
	commitments, err := rpcClient.GetCommitments(cmutxos)
	if err != nil {
		logger.Println("error:", err)
		return userSendResponse, err
	}

	exchangeOffer, err := getexchangeofferwb(sendAsset, sendAmount, offerAsset, commitments)
	if err != nil {
		logger.Println("error:", err)
		return userSendResponse, err
	}

	if (offerDetail.Cost != exchangeOffer.Cost) ||
		(offerDetail.Fee != exchangeOffer.Fee) {
		err := fmt.Errorf("quatation has changed: old (cost:%d, fee:%d) => new (cost:%d, fee:%d)",
			offerDetail.Cost, offerDetail.Fee, exchangeOffer.Cost, exchangeOffer.Fee)
		logger.Println("error:", err)
		return userSendResponse, err
	}
	offerDetail.ID = exchangeOffer.GetID()
	offerDetail.Transaction = exchangeOffer.Transaction
	commitments = append(exchangeOffer.Commitments, commitments...)

	tx, err := appendTransactionInfoWB(sendToAddr, sendAsset, sendAmount, offerAsset, offerDetail, ofutxos, sautxos)
	if err != nil {
		logger.Println("error:", err)
		return userSendResponse, err
	}

	blindtx, _, err := rpcClient.RequestAndCastString("blindrawtransaction", tx, commitments)
	if err != nil {
		logger.Println("RPC/blindrawtransaction error:", err, fmt.Sprintf("\n\tparam :%#v", tx))
		return userSendResponse, err
	}

	var signedtx rpc.SignedTransaction
	_, err = rpcClient.RequestAndUnmarshalResult(&signedtx, "signrawtransaction", blindtx)
	if err != nil {
		logger.Println("RPC/signrawtransaction error:", err, fmt.Sprintf("\n\tparam :%#v", blindtx))
		return userSendResponse, err
	}

	submitRes, err := submitexchange(signedtx.Hex)
	if err != nil {
		userSendResponse.Result = false
		userSendResponse.Message = fmt.Sprintf("fail ADDR:%s TxID:%s\nerr:%#v", sendToAddr, offerID, err)
	} else {
		userSendResponse.Result = true
		userSendResponse.Message = fmt.Sprintf("success ADDR:%s TxID:%s ", sendToAddr, submitRes.TransactionId)
	}

	delete(quotationList, quotationID)

	lockList.UnlockUnspentList(cmutxos)

	return userSendResponse, err
}

func doSendWithNoBlinding(offerID string, sendToAddr string) (UserSendResponse, error) {
	logger.Println(fmt.Sprintf("doSendWithNoBlinding start. (%s, %s)", offerID, sendToAddr))

	var userSendResponse UserSendResponse

	quotationID, offerAsset, err := getQuotation(quotationList, offerID)
	if err != nil {
		logger.Println("error:", err)
		return userSendResponse, err
	}

	offerDetail := quotationList[quotationID].Offer[offerAsset]
	sendAsset := quotationList[quotationID].RequestAsset
	sendAmount := quotationList[quotationID].RequestAmount

	exchangeOffer, err := getexchangeoffer(sendAsset, sendAmount, offerAsset)
	if err != nil {
		logger.Println("error:", err)
		return userSendResponse, err
	}

	if (offerDetail.Cost != exchangeOffer.Cost) ||
		(offerDetail.Fee != exchangeOffer.Fee) {
		err := fmt.Errorf("quatation has changed: old (cost:%d, fee:%d) => new (cost:%d, fee:%d)",
			offerDetail.Cost, offerDetail.Fee, exchangeOffer.Cost, exchangeOffer.Fee)
		logger.Println("error:", err)
		return userSendResponse, err
	}
	offerDetail.ID = exchangeOffer.GetID()
	offerDetail.Transaction = exchangeOffer.Transaction

	utxos, err := rpcClient.SearchUnspent(lockList, offerAsset, offerDetail.Cost+offerDetail.Fee, false)
	if err != nil {
		logger.Println("error:", err)
		return userSendResponse, err
	}

	tx, err := appendTransactionInfo(sendToAddr, sendAsset, sendAmount, offerAsset, offerDetail, utxos)
	if err != nil {
		logger.Println("error:", err)
		return userSendResponse, err
	}

	var signedtx rpc.SignedTransaction
	_, err = rpcClient.RequestAndUnmarshalResult(&signedtx, "signrawtransaction", tx)
	if err != nil {
		logger.Println("RPC/signrawtransaction error:", err, fmt.Sprintf("\n\tparam :%#v", tx))
		return userSendResponse, err
	}

	submitRes, err := submitexchange(signedtx.Hex)
	if err != nil {
		userSendResponse.Result = false
		userSendResponse.Message = fmt.Sprintf("fail ADDR:%s TxID:%s\nerr:%#v", sendToAddr, offerID, err)
	} else {
		userSendResponse.Result = true
		userSendResponse.Message = fmt.Sprintf("success ADDR:%s TxID:%s ", sendToAddr, submitRes.TransactionId)
	}

	delete(quotationList, quotationID)

	lockList.UnlockUnspentList(utxos)

	return userSendResponse, err
}

func getQuotation(ql map[string]Quotation, offerID string) (string, string, error) {
	var quotationID string
	var offerAsset string
	var found bool = false

	for i, v := range ql {
		for k, w := range v.Offer {
			if w.ID == offerID {
				offerAsset = k
				found = true
				break
			}
		}
		if found {
			quotationID = i
			break
		}
	}
	if !found {
		return "", "", fmt.Errorf("offerID not found [%s]", offerID)
	}
	return quotationID, offerAsset, nil
}

func getLockingKey(txid string, vout int64) string {
	return fmt.Sprintf("%s:%d", txid, vout)
}

func getAmount(ul rpc.UnspentList) int64 {
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

func isConfidential(addr string) (bool, error) {
	var validAddr rpc.ValidatedAddress

	_, err := rpcClient.RequestAndUnmarshalResult(&validAddr, "validateaddress", addr)
	if err != nil {
		return false, err
	}

	if validAddr.IsValid == false {
		return false, fmt.Errorf("invalid address [%s]", addr)
	}

	if addr == validAddr.Unconfidential {
		return false, nil
	}

	return true, nil
}

func getexchangerate(requestAsset string, requestAmount int64, offerAsset string) (lib.ExchangeRateResponse, error) {
	var rateRes lib.ExchangeRateResponse
	var rateReq lib.ExchangeRateRequest
	rateReq.Request = make(map[string]int64)
	rateReq.Request[requestAsset] = requestAmount
	rateReq.Offer = offerAsset

	_, err := callExchangerAPI(exchangeRateURL, rateReq, &rateRes)

	if err != nil {
		logger.Println("json#Marshal error:", err)
		logger.Println("----:%#v", rateRes)
	}
	return rateRes, err
}

func getexchangeofferwb(requestAsset string, requestAmount int64, offerAsset string, commitments []string) (lib.ExchangeOfferWBResponse, error) {
	var offerRes lib.ExchangeOfferWBResponse
	var offerReq lib.ExchangeOfferWBRequest
	offerReq.Request = make(map[string]int64)
	offerReq.Request[requestAsset] = requestAmount
	offerReq.Offer = offerAsset
	offerReq.Commitments = commitments

	_, err := callExchangerAPI(exchangeOfferWBURL, offerReq, &offerRes)

	if err != nil {
		logger.Println("json#Marshal error:", err)
	}
	return offerRes, err
}

func getexchangeoffer(requestAsset string, requestAmount int64, offerAsset string) (lib.ExchangeOfferResponse, error) {
	var offerRes lib.ExchangeOfferResponse
	var offerReq lib.ExchangeOfferRequest
	offerReq.Request = make(map[string]int64)
	offerReq.Request[requestAsset] = requestAmount
	offerReq.Offer = offerAsset

	_, err := callExchangerAPI(exchangeOfferURL, offerReq, &offerRes)

	if err != nil {
		logger.Println("json#Marshal error:", err)
	}
	return offerRes, err
}

func submitexchange(tx string) (lib.SubmitExchangeResponse, error) {
	var submitReq lib.SubmitExchangeRequest
	var submitRes lib.SubmitExchangeResponse
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
	utxoLockDuration = time.Duration(int64(conf.GetNumber("timeout", defaultTimeout))) * time.Second

	exchangeRateURL = "http://127.0.0.1" + exchangerConf.GetString("laddr", defaultCharlieListen) + "/getexchangerate/"
	exchangeOfferWBURL = "http://127.0.0.1" + exchangerConf.GetString("laddr", defaultCharlieListen) + "/getexchangeofferwb/"
	exchangeOfferURL = "http://127.0.0.1" + exchangerConf.GetString("laddr", defaultCharlieListen) + "/getexchangeoffer/"
	exchangeSubmitURL = "http://127.0.0.1" + exchangerConf.GetString("laddr", defaultCharlieListen) + "/submitexchange/"
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
