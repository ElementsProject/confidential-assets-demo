// Copyright (c) 2017 DG Lab
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

/**
 * Helper functions for the RPC utility
 */
package rpc

import (
	"fmt"
	"time"
)

// Lock lists to keep referenced utxos (potentially to-be-spent)

type LockList map[string]time.Time

var utxoLockDuration time.Duration

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

func getLockingKey(txid string, vout int64) string {
	return fmt.Sprintf("%s:%d", txid, vout)
}

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

func (ll LockList) Unlock(txid string, vout int64) {
	key := getLockingKey(txid, vout)
	delete(ll, key)

	return
}

func (ll LockList) Sweep() {
	now := time.Now()
	for k, v := range ll {
		if v.Sub(now) < 0 {
			delete(ll, k)
		}
	}
}

func (ll LockList) UnlockUnspentList(ul UnspentList) {
	for _, u := range ul {
		ll.Unlock(u.Txid, u.Vout)
	}
}



func (rpc* Rpc) GetNewAddr(confidential bool) (string, error) {
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

/**
 * Extract the commitments from a list of UTXOs and return these
 * as an array of hex strings.
 */
func (rpc* Rpc) GetCommitments(utxos UnspentList) ([]string, error) {
	var commitments []string = make([]string, len(utxos))

	for i, u := range utxos {
		commitments[i] = u.AssetCommitment
	}
	return commitments, nil
}
