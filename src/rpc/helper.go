// Copyright (c) 2017 DG Lab
// Distributed under the MIT software license, see the accompanying
// file COPYING or http://www.opensource.org/licenses/mit-license.php.

/**
 * Helper functions for the RPC utility
 */
package rpc

import (
	"fmt"
)

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
