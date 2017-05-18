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
	"fmt"
	"io/ioutil"
	"lib"
	"log"
	"net/http"
	"os"
	"os/exec"
	"rpc"
	"strconv"
	"strings"
	"time"
)

// UserWalletInfoRequest is a structure that represents the web-form for "/walletinfo" request.
type UserWalletInfoRequest struct {
}

// UserOfferRequest is a structure that represents the web-form for "/offer" request.
type UserOfferRequest struct {
	Asset string `json:"asset"`
	Cost  int64  `json:"cost"`
}

// UserSendRequest is a structure that represents the web-form for "/send" request.
type UserSendRequest struct {
	ID   string `json:"id"`
	Addr string `json:"addr"`
}

// UserOfferResByAsset is a structure for UserOfferResponse.
type UserOfferResByAsset struct {
	Fee         int64  `json:"fee"`
	Cost        int64  `json:"cost"`
	ID          string `json:"id"`
	Transaction string `json:"-"`
}

// UserOfferResponse is a map that represents the response for "/offer" request.
type UserOfferResponse map[string]UserOfferResByAsset

// UserSendResponse is a structure that represents the response for "/send" request.
type UserSendResponse struct {
	Result  bool   `json:"result"`
	Message string `json:"message"`
}

// UserWalletInfoResponse is a structure that represents the response for "/walletinfo" request.
type UserWalletInfoResponse struct {
	Balance rpc.BalanceMap `json:"balance"`
}

type quotation struct {
	RequestAsset  string
	RequestAmount int64
	Offer         map[string]UserOfferResByAsset
}

func (e *quotation) getID() string {
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
	defaultRPCURL        = "http://127.0.0.1:10000"
	defaultRPCUser       = "user"
	defaultRPCPass       = "pass"
	defaultLocalAddr     = ":8000"
	defaultTxPath        = "elements-tx"
	defaultTxOption      = ""
	defaultTimeout       = 600
	exchangerName        = "charlie"
	defaultExchLocalAddr = ":8020"
)

var logger = log.New(os.Stdout, myActorName+":", log.LstdFlags+log.Lshortfile)
var conf = democonf.NewDemoConf(myActorName)
var assetIDMap = make(map[string]string)
var lockList = make(rpc.LockList)
var rpcClient *rpc.Rpc
var elementsTxCommand string
var elementsTxOption string
var localAddr string
var quotationList = make(map[string]quotation)
var exchangerConf = democonf.NewDemoConf(exchangerName)
var exchangeRateURL string
var exchangeOfferWBURL string
var exchangeOfferURL string
var exchangeSubmitURL string

var handlerList = map[string]interface{}{
	"/walletinfo": doWalletInfo,
	"/offer":      doOffer,
	"/send":       doSend,
}

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

func doWalletInfo(reqForm UserWalletInfoRequest) (UserWalletInfoResponse, error) {
	var walletInfoRes UserWalletInfoResponse

	balance, err := getMyBalance()
	if err != nil {
		logger.Println("error:", err)
		return walletInfoRes, err
	}

	walletInfoRes.Balance = balance

	return walletInfoRes, nil
}

func chooseKnownAssets(b rpc.BalanceMap) {
	for k := range b {
		if _, ok := assetIDMap[k]; !ok {
			delete(b, k)
		}
	}
	return
}

func doOffer(userOfferRequest UserOfferRequest) (UserOfferResponse, error) {
	userOfferResponse := make(UserOfferResponse)
	var quot quotation

	requestAsset := userOfferRequest.Asset
	requestAmount := userOfferRequest.Cost

	balance, err := getMyBalance()
	if err != nil {
		logger.Println("error:", err)
		return nil, err
	}
	quot.RequestAsset = requestAsset
	quot.RequestAmount = requestAmount
	quot.Offer = make(map[string]UserOfferResByAsset)
	offerExists := false
	for offerAsset := range balance {
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
		quot.Offer[offerAsset] = offerByAsset
	}
	if offerExists {
		quotationList[quot.getID()] = quot
	}

	return userOfferResponse, nil
}

func appendTransactionInfo(sendToAddr string, sendAsset string, sendAmount int64, offerAsset string, offerDetail UserOfferResByAsset, utxos rpc.UnspentList) (string, error) {
	template := offerDetail.Transaction
	cost := offerDetail.Cost
	fee := offerDetail.Fee
	change := utxos.GetAmount() - (cost + fee)
	params := []string{}

	if elementsTxOption != "" {
		params = append(params, elementsTxOption)
	}
	params = append(params, template)

	for _, u := range utxos {
		txin := "in=" + u.Txid + ":" + strconv.FormatInt(u.Vout, 10)
		params = append(params, txin)
	}

	if 0 < change {
		addrChange, err := rpcClient.GetNewAddr(false)
		if err != nil {
			return "", err
		}
		outAddrChange := "outaddr=" + strconv.FormatInt(change, 10) + ":" + addrChange + ":" + assetIDMap[offerAsset]
		params = append(params, outAddrChange)
	}
	outAddrSend := "outaddr=" + strconv.FormatInt(sendAmount, 10) + ":" + sendToAddr + ":" + assetIDMap[sendAsset]
	outAddrFee := "outscript=" + strconv.FormatInt(fee, 10) + "::" + assetIDMap[offerAsset]
	params = append(params, outAddrSend, outAddrFee)

	out, err := exec.Command(elementsTxCommand, params...).Output()

	if err != nil {
		logger.Println("elements-tx error:", err, params, out)
		return "", err
	}

	txTemplate := strings.TrimRight(string(out), "\n")
	return txTemplate, nil
}

func appendTransactionInfoWB(sendToAddr string, sendAsset string, sendAmount int64, offerAsset string, offerDetail UserOfferResByAsset, utxos rpc.UnspentList, loopbackUtxos rpc.UnspentList) (string, error) {
	template := offerDetail.Transaction
	cost := offerDetail.Cost
	fee := offerDetail.Fee
	change := utxos.GetAmount() - (cost + fee)
	params := []string{}
	lbChange := loopbackUtxos.GetAmount()

	if elementsTxOption != "" {
		params = append(params, elementsTxOption)
	}
	params = append(params, template)

	for _, u := range utxos {
		txin := "in=" + u.Txid + ":" + strconv.FormatInt(u.Vout, 10)
		params = append(params, txin)
	}
	for _, u := range loopbackUtxos {
		txin := "in=" + u.Txid + ":" + strconv.FormatInt(u.Vout, 10)
		params = append(params, txin)
	}

	if 0 < change {
		addrChange, err := rpcClient.GetNewAddr(true)
		if err != nil {
			return "", err
		}
		outAddrChange := "outaddr=" + strconv.FormatInt(change, 10) + ":" + addrChange + ":" + assetIDMap[offerAsset]
		params = append(params, outAddrChange)
	}
	if 0 < lbChange {
		addrLbChange, err := rpcClient.GetNewAddr(true)
		if err != nil {
			return "", err
		}
		outAddrLbChange := "outaddr=" + strconv.FormatInt(lbChange, 10) + ":" + addrLbChange + ":" + assetIDMap[sendAsset]
		params = append(params, outAddrLbChange)
	}
	outAddrSend := "outaddr=" + strconv.FormatInt(sendAmount, 10) + ":" + sendToAddr + ":" + assetIDMap[sendAsset]
	params = append(params, outAddrSend)

	out, err := exec.Command(elementsTxCommand, params...).Output()

	if err != nil {
		logger.Println("elements-tx error:", err, params, out)
		return "", err
	}

	txTemplate := strings.TrimRight(string(out), "\n")
	return txTemplate, nil
}

func doSend(reqForm UserSendRequest) (UserSendResponse, error) {
	var userSendResponse UserSendResponse

	offerID := reqForm.ID
	sendToAddr := reqForm.Addr
	isConfidential, err := isConfidential(sendToAddr)
	if err != nil {
		logger.Println("error:", err)
		return userSendResponse, err
	}

	if isConfidential {
		userSendResponse, err = doSendWithBlinding(offerID, sendToAddr)
	} else {
		userSendResponse, err = doSendWithNoBlinding(offerID, sendToAddr)
	}

	return userSendResponse, err
}

func doSendWithBlinding(offerID string, sendToAddr string) (UserSendResponse, error) {
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
		err = fmt.Errorf("quotation has changed: old (cost:%d, fee:%d) => new (cost:%d, fee:%d)",
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

	blindtx, _, err := rpcClient.RequestAndCastString("blindrawtransaction", tx, true, commitments)
	if err != nil {
		logger.Println("RPC/blindrawtransaction error:", err, tx)
		return userSendResponse, err
	}

	var signedtx rpc.SignedTransaction
	_, err = rpcClient.RequestAndUnmarshalResult(&signedtx, "signrawtransaction", blindtx)
	if err != nil {
		logger.Println("RPC/signrawtransaction error:", err, blindtx)
		return userSendResponse, err
	}

	submitRes, err := submitexchange(signedtx.Hex)
	if err != nil {
		userSendResponse.Result = false
		userSendResponse.Message = fmt.Sprintf("fail ADDR:%s TxID:%s\nerr:%#v", sendToAddr, offerID, err)
	} else {
		userSendResponse.Result = true
		userSendResponse.Message = fmt.Sprintf("success ADDR:%s TxID:%s", sendToAddr, submitRes.TransactionID)
	}

	delete(quotationList, quotationID)

	lockList.UnlockUnspentList(cmutxos)

	return userSendResponse, err
}

func doSendWithNoBlinding(offerID string, sendToAddr string) (UserSendResponse, error) {
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
		err = fmt.Errorf("quotation has changed: old (cost:%d, fee:%d) => new (cost:%d, fee:%d)",
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
		logger.Println("RPC/signrawtransaction error:", err, tx)
		return userSendResponse, err
	}

	submitRes, err := submitexchange(signedtx.Hex)
	if err != nil {
		userSendResponse.Result = false
		userSendResponse.Message = fmt.Sprintf("fail ADDR:%s TxID:%s\nerr:%#v", sendToAddr, offerID, err)
	} else {
		userSendResponse.Result = true
		userSendResponse.Message = fmt.Sprintf("success ADDR:%s TxID:%s", sendToAddr, submitRes.TransactionID)
	}

	delete(quotationList, quotationID)

	lockList.UnlockUnspentList(utxos)

	return userSendResponse, err
}

func getQuotation(ql map[string]quotation, offerID string) (string, string, error) {
	var quotationID string
	var offerAsset string
	found := false

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

	if !validAddr.IsValid {
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
		logger.Println("json#Marshal error:", err, rateRes)
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

func callExchangerAPI(targetURL string, param interface{}, result interface{}) (*http.Response, error) {
	encodedRequest, err := json.Marshal(param)
	if err != nil {
		logger.Println("json#Marshal error:", err)
		return nil, err
	}
	client := &http.Client{}
	reqBody := string(encodedRequest)
	req, err := http.NewRequest("POST", targetURL, bytes.NewBufferString(reqBody))
	req.Header.Set("Content-Type", "text/plain")
	logger.Println(req)
	if err != nil {
		logger.Println("http#NewRequest error", err)
		return nil, err
	}
	res, err := client.Do(req)
	logger.Println(res)
	if err != nil {
		logger.Println("http.Client#Do error:", err)
		return nil, err
	}
	body, err := ioutil.ReadAll(res.Body)
	defer func() {
		e := res.Body.Close()
		if e != nil {
			logger.Println("error:", e)
		}
	}()
	if err != nil {
		logger.Println("ioutil#ReadAll error:", err)
		return res, err
	}
	err = json.Unmarshal(body, result)

	return res, err
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

	exLocalAddr := "http://127.0.0.1" + exchangerConf.GetString("laddr", defaultExchLocalAddr)
	exchangeRateURL = exLocalAddr + "/getexchangerate/"
	exchangeOfferWBURL = exLocalAddr + "/getexchangeofferwb/"
	exchangeOfferURL = exLocalAddr + "/getexchangeoffer/"
	exchangeSubmitURL = exLocalAddr + "/submitexchange/"
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
