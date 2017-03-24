// Copyright (c) 2017 DG Lab
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

// democonf project democonf.go
package democonf

import (
	"encoding/json"
	"log"
	"os"
)

type DemoConf struct {
	Data   map[string]interface{}
	logger *log.Logger
}

func NewDemoConf(section string) *DemoConf {
	conf := new(DemoConf)
	conf.logger = log.New(os.Stdout, "DemoConf:", log.LstdFlags)
	dir, _ := os.Getwd()
	file, err := os.Open(dir + "/democonf.json")
	if err != nil {
		conf.logger.Println("os#Open error", err)
		return conf
	}
	dec := json.NewDecoder(file)
	var j map[string]map[string]interface{}
	err = dec.Decode(&j)
	if err != nil {
		conf.logger.Println("decode error", err)
		return conf
	}
	val, ok := j[section]
	if !ok {
		conf.logger.Println("not found section", section)
		return conf
	}
	conf.Data = val
	return conf
}

func (conf *DemoConf) GetString(key string, defaultValue string) string {
	val, ok := conf.Data[key]
	if !ok {
		conf.logger.Println("key not found. Key:", key)
		return defaultValue
	}
	str, ok := val.(string)
	if !ok {
		conf.logger.Printf("type is not string. Type: %T, Value: %+v\n", val, val)
		return defaultValue
	}
	return str
}

func (conf *DemoConf) GetNumber(key string, defaultValue float64) float64 {
	val, ok := conf.Data[key]
	if !ok {
		conf.logger.Println("key not found. Key:", key)
		return defaultValue
	}
	num, ok := val.(float64)
	if !ok {
		conf.logger.Printf("type is not number. Type: %T, Value: %+v\n", val, val)
		return defaultValue
	}
	return num
}

func (conf *DemoConf) GetBool(key string, defaultValue bool) bool {
	val, ok := conf.Data[key]
	if !ok {
		conf.logger.Println("key not found. Key:", key)
		return defaultValue
	}
	b, ok := val.(bool)
	if !ok {
		conf.logger.Printf("type is not bool. Type: %T, Value: %+v\n", val, val)
		return defaultValue
	}
	return b
}

func (conf *DemoConf) GetInterface(key string, result interface{}) {
	val, ok := conf.Data[key]
	if !ok {
		conf.logger.Println("key not found. Key:", key)
		return
	}
	var bs []byte
	m, ok := val.(map[string]interface{})
	if !ok {
		a, ok := val.([]interface{})
		if !ok {
			conf.logger.Printf("type is neither map[string]interface{} nor []interface{}. Type: %T, Value: %+v\n", val, val)
			return
		} else {
			bs, _ = json.Marshal(a)
		}
	} else {
		bs, _ = json.Marshal(m)
	}
	err := json.Unmarshal(bs, result)
	if err != nil {
		conf.logger.Printf("json#Unmarshal error. %+v", err)
		return
	}
	return
}
