// Copyright (c) 2017 DG Lab
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

/**
 * HTTP related functions
 */
package lib

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"time"
)

type ExchangeOfferRequest struct {
	Request map[string]int64 `json:"request"`
	Offer   string           `json:"offer"`
}

type ExchangeOfferWBRequest struct {
	Request     map[string]int64 `json:"request"`
	Offer       string           `json:"offer"`
	Commitments []string         `json:"commitments"`
}

type ExchangeOfferResponse struct {
	Fee         int64  `json:"fee"`
	AssetLabel  string `json:"assetid"`
	Cost        int64  `json:"cost"`
	Transaction string `json:"tx"`
}

func (u *ExchangeOfferResponse) GetID() string {
	tx := u.Transaction
	now := time.Now().Unix()
	nowba := make([]byte, binary.MaxVarintLen64)
	binary.PutVarint(nowba, now)
	target := append([]byte(tx), nowba...)
	hash := sha256.Sum256(target)
	id := fmt.Sprintf("%x", hash)
	return id
}

type ExchangeOfferWBResponse struct {
	Fee         int64    `json:"fee"`
	AssetLabel  string   `json:"assetid"`
	Cost        int64    `json:"cost"`
	Transaction string   `json:"tx"`
	Commitments []string `json:"commitments"`
}

func (u *ExchangeOfferWBResponse) GetID() string {
	tx := u.Transaction
	now := time.Now().Unix()
	nowba := make([]byte, binary.MaxVarintLen64)
	binary.PutVarint(nowba, now)
	target := append([]byte(tx), nowba...)
	hash := sha256.Sum256(target)
	id := fmt.Sprintf("%x", hash)
	return id
}

type ExchangeRateRequest struct {
	Request map[string]int64 `json:"request"`
	Offer   string           `json:"offer"`
}

type ExchangeRateResponse struct {
	Fee        int64  `json:"fee"`
	AssetLabel string `json:"assetid"`
	Cost       int64  `json:"cost"`
}

func (u *ExchangeRateResponse) GetID() string {
	tfac := fmt.Sprintf("%d:%s:%d", u.Fee, u.AssetLabel, u.Cost)
	now := time.Now().Unix()
	nowba := make([]byte, binary.MaxVarintLen64)
	binary.PutVarint(nowba, now)
	target := append([]byte(tfac), nowba...)
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

type ErrorResponse struct {
	Result  bool   `json:"result"`
	Message string `json:"message"`
}

type CyclicProcess struct {
	Handler  func()
	Interval int
}

var logger *log.Logger

func SetLogger(loggerIn *log.Logger) {
	logger = loggerIn
}

func createErrorByteArray(e error) []byte {
	if e == nil {
		e = errors.New("error occured (fake)")
	}
	res := ErrorResponse{
		Result:  false,
		Message: fmt.Sprintf("%s", e),
	}
	b, _ := json.Marshal(res)
	return b
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
			logger.Println(fmt.Sprintf("ioutil#ReadAll error:%#v", err))
			_, _ = w.Write(nil)
			return
		}
		res, err = f(nil, string(req))

	default:
		status = http.StatusMethodNotAllowed
	}

	if err != nil {
		logger.Println(fmt.Sprintf("error:%#v", err))
		status = http.StatusInternalServerError
		if res != nil {
			res = createErrorByteArray(err)
		}
	}

	w.WriteHeader(status)
	_, err = w.Write(res)
	if err != nil {
		logger.Println(fmt.Sprintf("w#Write Error:%#v", err))
		return
	}
}

func generateMuxHandler(h func(url.Values, string) ([]byte, error)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		handler(w, r, h)
		return
	}
}

func StartHttpServer(laddr string, handlers map[string]func(url.Values, string) ([]byte, error), filepath string) (net.Listener, error) {
	listener, err := net.Listen("tcp", laddr)
	if err != nil {
		return listener, err
	}

	mux := http.NewServeMux()
	for p, h := range handlers {
		f := generateMuxHandler(h)
		mux.HandleFunc(p, f)
	}

	mux.Handle("/", http.FileServer(http.Dir(filepath)))
	go http.Serve(listener, mux)

	return listener, err
}
