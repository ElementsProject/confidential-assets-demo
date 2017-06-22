// Copyright (c) 2017 DG Lab
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

// Package rpc Helper functions for the RPC utility
package rpc

import (
	"fmt"
	"sort"
	"time"
)

// LockList to keep referenced utxos (potentially to-be-spent)
type LockList map[string]time.Time

var utxoLockDuration time.Duration

// SetUtxoLockDuration set utxoLockDuration
func SetUtxoLockDuration(utxoLockDurationIn time.Duration) {
	utxoLockDuration = utxoLockDurationIn
}

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

// GetAmount get total amount.
func (ul UnspentList) GetAmount() int64 {
	var totalAmount = int64(0)

	for _, u := range ul {
		totalAmount += u.Amount
	}

	return totalAmount
}

func getLockingKey(txid string, vout int64) string {
	return fmt.Sprintf("%s:%d", txid, vout)
}

// Lock lock utxo.
func (ll LockList) Lock(txid string, vout int64) bool {
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

// Unlock unlock utxo.
func (ll LockList) Unlock(txid string, vout int64) {
	key := getLockingKey(txid, vout)
	delete(ll, key)

	return
}

// Sweep delete timeout
func (ll LockList) Sweep() {
	now := time.Now()
	for k, v := range ll {
		if v.Sub(now) < 0 {
			delete(ll, k)
		}
	}
}

// UnlockUnspentList unlock utxos.
func (ll LockList) UnlockUnspentList(ul UnspentList) {
	for _, u := range ul {
		ll.Unlock(u.Txid, u.Vout)
	}
}

// GetNewAddr get new address, confidential or normal.
func (rpc *Rpc) GetNewAddr(confidential bool) (string, error) {
	var validAddr ValidatedAddress

	adr, _, err := rpc.RequestAndCastString("getnewaddress")
	if err != nil {
		return "", err
	}

	if confidential {
		return adr, nil
	}

	_, err = rpc.RequestAndUnmarshalResult(&validAddr, "validateaddress", adr)
	if err != nil {
		return "", err
	}

	if validAddr.Unconfidential == "" {
		return "", fmt.Errorf("unconfidential is empty")
	}

	return validAddr.Unconfidential, nil
}

// GetCommitments : Extract the commitments from a list of UTXOs and return these
// as an array of hex strings.
func (rpc *Rpc) GetCommitments(utxos UnspentList) ([]string, error) {
	var commitments = make([]string, len(utxos))

	for i, u := range utxos {
		commitments[i] = u.AssetCommitment
	}
	return commitments, nil
}

// SearchUnspent search unspent utxo.
func (rpc *Rpc) SearchUnspent(lockList LockList, requestAsset string, requestAmount int64, blinding bool) (UnspentList, error) {
	var totalAmount = int64(0)
	var ul UnspentList
	var utxos = make(UnspentList, 0)

	_, err := rpc.RequestAndUnmarshalResult(&ul, "listunspent", 1, 9999999, []string{}, false, requestAsset)
	if err != nil {
		return utxos, err
	}
	sort.Sort(sort.Reverse(ul))

	for _, u := range ul {
		if requestAmount < totalAmount {
			break
		}
		if blinding && (u.AssetCommitment == "") {
			continue
		}
		if !blinding && (u.AssetCommitment != "") {
			continue
		}
		if !(u.Spendable || u.Solvable) {
			continue
		}
		if !lockList.Lock(u.Txid, u.Vout) {
			continue
		}
		totalAmount += u.Amount
		utxos = append(utxos, u)
	}

	if requestAmount >= totalAmount {
		lockList.UnlockUnspentList(utxos)
		err = fmt.Errorf("no sufficient utxo")
		return utxos, err
	}

	return utxos, nil
}

// SearchMinimalUnspent search unspent minimal utxo.
func (rpc *Rpc) SearchMinimalUnspent(lockList LockList, requestAsset string, blinding bool) (UnspentList, error) {
	var ul UnspentList
	var utxos UnspentList

	_, err := rpc.RequestAndUnmarshalResult(&ul, "listunspent", 1, 9999999, []string{}, false, requestAsset)
	if err != nil {
		return utxos, err
	}

	if ul.Len() == 0 {
		err := fmt.Errorf("no utxo [%s]", requestAsset)
		return utxos, err
	}

	sort.Sort(ul)
	var start = 0
	var found = false
	for i, u := range ul {
		if blinding && (u.AssetCommitment == "") {
			continue
		}
		if !blinding && (u.AssetCommitment != "") {
			continue
		}
		if !(u.Spendable || u.Solvable) {
			continue
		}
		if !lockList.Lock(u.Txid, u.Vout) {
			continue
		}

		start = i
		found = true
		break
	}
	if !found {
		err := fmt.Errorf("no utxo [%s]", requestAsset)
		return utxos, err
	}

	minUnspent := ul[start]
	if ul.Len() == start+1 {
		utxos = append(utxos, minUnspent)
		return utxos, nil
	}

	for _, u := range ul[start+1:] {
		if u.Amount != minUnspent.Amount {
			break
		}
		if blinding && (u.AssetCommitment == "") {
			continue
		}
		if !blinding && (u.AssetCommitment != "") {
			continue
		}
		if !(u.Spendable || u.Solvable) {
			continue
		}
		if !lockList.Lock(u.Txid, u.Vout) {
			continue
		}

		lockList.Unlock(minUnspent.Txid, minUnspent.Vout)
		minUnspent = u
	}

	utxos = append(utxos, minUnspent)
	return utxos, nil
}
