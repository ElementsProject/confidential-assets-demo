// Copyright (c) 2017 DG Lab
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

// rpc
package rpc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"
)

type Rpc struct {
	Url  string
	User string
	Pass string
	View bool
}

type RpcRequest struct {
	Jsonrpc string        `json:"jsonrpc,"`
	Id      string        `json:"id,"`
	Method  string        `json:"method,"`
	Params  []interface{} `json:"params,"`
}

type RpcResponse struct {
	Result interface{} `json:"result,"`
	Error  interface{} `json:"error,"`
	Id     string      `json:"id,"`
}

type RpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (res *RpcResponse) UnmarshalError() (RpcError, error) {
	var rerr RpcError
	if res.Error == nil {
		return rerr, fmt.Errorf("RpcResponse Error is nil.")
	}
	data, ok := res.Error.(map[string]interface{})
	if !ok {
		return rerr, fmt.Errorf("RpcResponse Error is not map[string]interface{}")
	}
	bs, _ := json.Marshal(data)
	json.Unmarshal(bs, &rerr)
	return rerr, nil
}

func (res *RpcResponse) UnmarshalResult(result interface{}) error {
	if res.Result == nil {
		return fmt.Errorf("RpcResponse Result is nil.")
	}
	var bs []byte
	m, ok := res.Result.(map[string]interface{})
	if !ok {
		arr, ok := res.Result.([]interface{})
		if !ok {
			return fmt.Errorf("RpcResponse Result is neither map[string]interface{} nor []interface{}")
		} else {
			bs, _ = json.Marshal(arr)
		}
	} else {
		bs, _ = json.Marshal(m)
	}
	err := json.Unmarshal(bs, result)
	if err != nil {
		return err
	}
	return nil
}

func NewRpc(url, user, pass string) *Rpc {
	rpc := new(Rpc)
	rpc.Url = url
	rpc.User = user
	rpc.Pass = pass
	return rpc
}

func (rpc *Rpc) Request(method string, params ...interface{}) (RpcResponse, error) {
	var res RpcResponse
	if len(params) == 0 {
		params = []interface{}{}
	}
	id := fmt.Sprintf("%d", time.Now().Unix())
	req := &RpcRequest{"1.0", id, method, params}
	bs, _ := json.Marshal(req)
	if rpc.View {
		fmt.Printf("%s\n", bs)
	}
	client := &http.Client{}
	hreq, _ := http.NewRequest(http.MethodPost, rpc.Url, bytes.NewBuffer(bs))
	hreq.SetBasicAuth(rpc.User, rpc.Pass)
	hres, err := client.Do(hreq)
	if err != nil {
		return res, err
	}
	defer hres.Body.Close()
	body, _ := ioutil.ReadAll(hres.Body)
	if rpc.View {
		fmt.Printf("%d, %s\n", hres.StatusCode, body)
	}
	err = json.Unmarshal(body, &res)
	if err != nil || hres.StatusCode != http.StatusOK || res.Id != id {
		return res, fmt.Errorf("status:%v, error:%v, body:%s reqid:%v, resid:%v", hres.Status, err, body, id, res.Id)
	}
	return res, nil
}

func (rpc *Rpc) RequestAndUnmarshalResult(result interface{}, method string, params ...interface{}) (RpcResponse, error) {
	res, err := rpc.Request(method, params...)
	if err != nil {
		return res, err
	}
	err = res.UnmarshalResult(result)
	if err != nil {
		return res, err
	}
	return res, nil
}

func (rpc *Rpc) RequestAndCastNumber(method string, params ...interface{}) (float64, RpcResponse, error) {
	var num float64
	res, err := rpc.Request(method, params...)
	if err != nil {
		return num, res, err
	}
	num, ok := res.Result.(float64)
	if !ok {
		return num, res, fmt.Errorf("RpcResponse Result cast error:%+v", res.Result)
	}
	return num, res, nil
}

func (rpc *Rpc) RequestAndCastString(method string, params ...interface{}) (string, RpcResponse, error) {
	var str string
	res, err := rpc.Request(method, params...)
	if err != nil {
		return str, res, err
	}
	str, ok := res.Result.(string)
	if !ok {
		return str, res, fmt.Errorf("RpcResponse Result cast error:%+v", res.Result)
	}
	return str, res, nil
}

func (rpc *Rpc) RequestAndCastBool(method string, params ...interface{}) (bool, RpcResponse, error) {
	var b bool
	res, err := rpc.Request(method, params...)
	if err != nil {
		return b, res, err
	}
	b, ok := res.Result.(bool)
	if !ok {
		return b, res, fmt.Errorf("RpcResponse Result cast error:%+v", res.Result)
	}
	return b, res, nil
}
