// Copyright (c) 2017 DG Lab
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

/**
 * HTTP related functions
 */
package lib

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
)

type ErrorResponse struct {
	Result  bool   `json:"result"`
	Message string `json:"message"`
}

var logger *log.Logger

func setLogger(loggerIn *log.Logger) {
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
