// Copyright (c) 2017 DG Lab
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

/*
Package lib provides HTTP related functions.

There is a very simple http-form/json framework.
And it is also provides cyclic process management.
*/
package lib

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ExchangeRateRequest is a structure that represents the JSON-API request.
type ExchangeRateRequest struct {
	Request map[string]int64 `json:"request"`
	Offer   string           `json:"offer"`
}

// ExchangeRateResponse is a structure that represents the JSON-API response.
type ExchangeRateResponse struct {
	Fee        int64  `json:"fee"`
	AssetLabel string `json:"assetid"`
	Cost       int64  `json:"cost"`
}

// GetID returns ID of ExchangeRateResponse instance.
func (u *ExchangeRateResponse) GetID() string {
	return generateID(fmt.Sprintf("%d:%s:%d", u.Fee, u.AssetLabel, u.Cost))
}

func generateID(key string) string {
	now := time.Now().Unix()
	nowba := make([]byte, binary.MaxVarintLen64)
	binary.PutVarint(nowba, now)
	target := append([]byte(key), nowba...)
	hash := sha256.Sum256(target)
	id := fmt.Sprintf("%x", hash)
	return id
}

// ExchangeOfferRequest is a structure that represents the JSON-API request.
type ExchangeOfferRequest struct {
	Request map[string]int64 `json:"request"`
	Offer   string           `json:"offer"`
}

// ExchangeOfferResponse is a structure that represents the JSON-API response.
type ExchangeOfferResponse struct {
	Fee         int64  `json:"fee"`
	AssetLabel  string `json:"assetid"`
	Cost        int64  `json:"cost"`
	Transaction string `json:"tx"`
}

// GetID returns ID of ExchangeOfferResponse instance.
func (u *ExchangeOfferResponse) GetID() string {
	return generateID(u.Transaction)
}

// ExchangeOfferWBRequest is a structure that represents the JSON-API request.
type ExchangeOfferWBRequest struct {
	Request     map[string]int64 `json:"request"`
	Offer       string           `json:"offer"`
	Commitments []string         `json:"commitments"`
}

// ExchangeOfferWBResponse is a structure that represents the JSON-API response.
type ExchangeOfferWBResponse struct {
	Fee         int64    `json:"fee"`
	AssetLabel  string   `json:"assetid"`
	Cost        int64    `json:"cost"`
	Transaction string   `json:"tx"`
	Commitments []string `json:"commitments"`
}

// GetID returns ID of ExchangeOfferWBResponse instance.
func (u *ExchangeOfferWBResponse) GetID() string {
	return generateID(u.Transaction)
}

// SubmitExchangeRequest is a structure that represents the JSON-API request.
type SubmitExchangeRequest struct {
	Transaction string `json:"tx"`
}

// SubmitExchangeResponse is a structure that represents the JSON-API response.
type SubmitExchangeResponse struct {
	TransactionID string `json:"txid"`
}

// ErrorResponse is a structure that represents the JSON-API response.
type ErrorResponse struct {
	Result  bool   `json:"result"`
	Message string `json:"message"`
}

// CyclicProcess is a structure that holds function and its calling interval.
type CyclicProcess struct {
	Handler  func()
	Interval int
}

var logger *log.Logger

// SetLogger sets logger.
func SetLogger(loggerIn *log.Logger) {
	logger = loggerIn
}

func createErrorByteArray(e error) []byte {
	if e == nil {
		e = fmt.Errorf("error occured (fake)")
	}
	res := ErrorResponse{
		Result:  false,
		Message: fmt.Sprintf("%s", e),
	}
	b, err := json.Marshal(res)
	if err != nil {
		logger.Println("error:", err)
	}
	return b
}

func handler(w http.ResponseWriter, r *http.Request, fi interface{}, n string) {

	w.Header().Add("Access-Control-Allow-Origin", "*")
	w.Header().Add("Access-Control-Allow-Methods", "GET")
	w.Header().Add("Access-Control-Allow-Headers", r.Header.Get("Access-Control-Request-Headers"))
	w.Header().Add("Access-Control-Max-Age", "-1")

	status := http.StatusOK
	createParam := formToFlatStruct
	var res interface{}
	var err error
	var fp0e reflect.Value

	defer func() {
		e := r.Body.Close()
		logger.Println("error:", e)
	}()

	switch r.Method {
	case "GET":
	case "POST":
		ct := strings.Split(""+r.Header.Get("Content-Type"), ";")[0]
		switch ct {
		case "application/x-www-form-urlencoded":
		case "application/json", "text/plain":
			createParam = jsonToStruct
		default:
			status = http.StatusBadRequest
			err = fmt.Errorf("content-type not allowed:%s", ct)
			logger.Println("error:", err)
			handleTermninate(w, res, status, err)
			return
		}
	default:
		status = http.StatusMethodNotAllowed
		err = fmt.Errorf("method not allowed:%s", r.Method)
		logger.Println("error:", err)
		handleTermninate(w, res, status, err)
		return
	}

	fp0e, err = createParam(r, fi)
	if err != nil {
		status = http.StatusInternalServerError
		logger.Println("error:", err)
		handleTermninate(w, res, status, err)
		return
	}

	fv := reflect.ValueOf(fi)
	logger.Println("start:", n)
	result := fv.Call([]reflect.Value{fp0e})
	logger.Println("end:", n)

	if err, ok := result[1].Interface().(error); ok {
		status = http.StatusInternalServerError
		logger.Println("error:", err)
		handleTermninate(w, res, status, err)
	}

	handleTermninate(w, result[0].Interface(), status, nil)

	return
}

func formToFlatStruct(r *http.Request, fi interface{}) (reflect.Value, error) {
	var formValue reflect.Value

	err := r.ParseForm()
	if err != nil {
		logger.Println("http.Request#ParseForm error:", err)
		return formValue, err
	}

	fv := reflect.ValueOf(fi)
	fp0t := fv.Type().In(0)
	fp0v := reflect.New(fp0t)
	formValue = fp0v.Elem()

	for i := 0; i < fp0t.NumField(); i++ {
		sfv := formValue.Field(i)

		if !sfv.CanSet() {
			continue
		}

		paramv := getParamByNameIgnoreCase(r.Form, fp0t.Field(i).Name)
		switch sfv.Kind() {
		case reflect.String:
			sfv.SetString(paramv)
		case reflect.Int64:
			val, err := strconv.ParseInt(paramv, 10, 64)
			if err != nil {
				break
			}
			sfv.SetInt(val)
		default:
		}
	}

	return formValue, nil
}

func getParamByNameIgnoreCase(form url.Values, key string) string {
	lkey := strings.ToLower(key)
	var value string
	for k, v := range form {
		if strings.ToLower(k) == lkey {
			value = v[0]
		}
	}
	return value
}

func jsonToStruct(r *http.Request, fi interface{}) (reflect.Value, error) {
	var fp0e reflect.Value

	reqBody, err := ioutil.ReadAll(r.Body)
	if err != nil {
		logger.Println("ioutil#ReadAll error:", err)
		return fp0e, err
	}

	fv := reflect.ValueOf(fi)
	fp0v := reflect.New(fv.Type().In(0))
	fp0i := fp0v.Interface()

	err = json.Unmarshal(reqBody, fp0i)
	if err != nil {
		logger.Println("json#Unmarshal error:", err)
		return fp0e, err
	}

	return fp0v.Elem(), nil
}

func handleTermninate(w http.ResponseWriter, resif interface{}, status int, err error) {
	if resif == nil && err != nil {
		resif = err
	}

	res, err := json.Marshal(resif)
	if err != nil {
		status = http.StatusInternalServerError
		logger.Println("json#Marshal error:", err)
		res = createErrorByteArray(err)
	}

	w.WriteHeader(status)
	_, err = w.Write(res)
	if err != nil {
		logger.Println("w#Write Error:", err)
		return
	}
}

func generateMuxHandler(h interface{}) func(http.ResponseWriter, *http.Request) {
	fv := reflect.ValueOf(h)
	n := runtime.FuncForPC(fv.Pointer()).Name()
	return func(w http.ResponseWriter, r *http.Request) {
		handler(w, r, h, n)
		return
	}
}

// StartHTTPServer binds specific URL and handler function. And it starts http server.
func StartHTTPServer(laddr string, handlers map[string]interface{}, filepath string) (net.Listener, error) {
	listener, err := net.Listen("tcp", laddr)
	if err != nil {
		return listener, err
	}

	mux := http.NewServeMux()
	for p, h := range handlers {
		hv := reflect.ValueOf(h)
		if !hv.IsValid() {
			return listener, fmt.Errorf("handler is invalid")
		}
		if hv.Kind() != reflect.Func {
			return listener, fmt.Errorf("each handler must be a function")
		}
		funcname := runtime.FuncForPC(hv.Pointer()).Name()
		ht := hv.Type()
		if ht.NumIn() != 1 {
			return listener, fmt.Errorf("[%s] must have one input. but it has %d", funcname, ht.NumIn())
		}
		hi0t := ht.In(0)
		if hi0t.Kind() != reflect.Struct {
			return listener, fmt.Errorf("[%s] input must be a struct", funcname)
		}
		if ht.NumOut() != 2 {
			return listener, fmt.Errorf("[%s] must have two output. but it has %d", funcname, ht.NumOut())
		}
		ho0t := ht.Out(0)
		if (ho0t.Kind() != reflect.Struct) && (ho0t.Kind() != reflect.Map) {
			return listener, fmt.Errorf("[%s] 1st output must be a struct or a map", funcname)
		}
		ho1t := ht.Out(1)
		if ho1t.Kind() != reflect.Interface {
			return listener, fmt.Errorf("[%s] 2nd output must be a interface", funcname)
		}
		errorType := reflect.TypeOf((*error)(nil)).Elem()
		if !ho1t.Implements(errorType) {
			return listener, fmt.Errorf("[%s] 2nd output must implements error", funcname)
		}

		f := generateMuxHandler(h)
		mux.HandleFunc(p, f)
	}

	mux.Handle("/", http.FileServer(http.Dir(filepath)))
	go func() {
		e := http.Serve(listener, mux)
		logger.Println("error:", e)
	}()

	return listener, err
}

// StartCyclicProc calls each function with each interval.
func StartCyclicProc(cps []CyclicProcess, stop *bool) {
	for _, cyclic := range cps {
		c := cyclic
		go func() {
			logger.Println("Loop interval:", c.Interval)
			for {
				time.Sleep(time.Duration(c.Interval) * time.Second)
				if *stop {
					break
				}
				c.Handler()
			}
		}()
	}
}

// WaitStopSignal waits stop flag to be true.
func WaitStopSignal(stop *bool) {
	for {
		time.Sleep(1 * time.Second)
		if *stop {
			break
		}
	}
}
