// Copyright 2018 Shift Devices AG
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bitbox02

import (
	"bytes"
	"math/big"

	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcutil/hdkeychain"
	"github.com/digitalbitbox/bitbox-wallet-app/backend/coins/btc"
	"github.com/digitalbitbox/bitbox-wallet-app/backend/coins/coin"
	coinpkg "github.com/digitalbitbox/bitbox-wallet-app/backend/coins/coin"
	"github.com/digitalbitbox/bitbox-wallet-app/backend/coins/eth"
	"github.com/digitalbitbox/bitbox-wallet-app/backend/coins/ltc"
	keystorePkg "github.com/digitalbitbox/bitbox-wallet-app/backend/keystore"
	"github.com/digitalbitbox/bitbox-wallet-app/backend/signing"
	"github.com/digitalbitbox/bitbox-wallet-app/util/errp"
	"github.com/digitalbitbox/bitbox02-api-go/api/firmware"
	"github.com/digitalbitbox/bitbox02-api-go/api/firmware/messages"
	"github.com/sirupsen/logrus"
)

type keystore struct {
	device        *Device
	configuration *signing.Configuration
	cosignerIndex int
	log           *logrus.Entry
}

// CosignerIndex implements keystore.Keystore.
func (keystore *keystore) CosignerIndex() int {
	return keystore.cosignerIndex
}

// SupportsAccount implements keystore.Keystore.
func (keystore *keystore) SupportsAccount(
	coin coin.Coin, multisig bool, meta interface{}) bool {
	switch coin.(type) {
	case *btc.Coin:
		if (coin.Code() == "ltc" || coin.Code() == "tltc") && !keystore.device.SupportsLTC() {
			return false
		}
		scriptType := meta.(signing.ScriptType)
		return !multisig && scriptType != signing.ScriptTypeP2PKH
	case *eth.Coin:
		return keystore.device.SupportsETH(coin.Code())
	default:
		return false
	}
}

// CanVerifyAddress implements keystore.Keystore.
func (keystore *keystore) CanVerifyAddress(configuration *signing.Configuration, coin coinpkg.Coin) (bool, bool, error) {
	optional := false
	switch coin.(type) {
	case *btc.Coin:
		_, ok := btcMsgCoinMap[coin.Code()]
		return ok, optional, nil
	case *eth.Coin:
		_, ok := ethMsgCoinMap[coin.Code()]
		return ok, optional, nil
	}
	return false, false, nil
}

// VerifyAddress implements keystore.Keystore.
func (keystore *keystore) VerifyAddress(
	configuration *signing.Configuration, coin coinpkg.Coin) error {
	canVerifyAddress, _, err := keystore.CanVerifyAddress(configuration, coin)
	if err != nil {
		return err
	}
	if !canVerifyAddress {
		panic("CanVerifyAddress must be true")
	}
	switch specificCoin := coin.(type) {
	case *btc.Coin:
		msgScriptType, ok := map[signing.ScriptType]messages.BTCScriptType{
			signing.ScriptTypeP2PKH:      messages.BTCScriptType_SCRIPT_P2PKH,
			signing.ScriptTypeP2WPKHP2SH: messages.BTCScriptType_SCRIPT_P2WPKH_P2SH,
			signing.ScriptTypeP2WPKH:     messages.BTCScriptType_SCRIPT_P2WPKH,
		}[configuration.ScriptType()]
		if !ok {
			panic("unsupported script type")
		}
		_, err = keystore.device.BTCPub(
			btcMsgCoinMap[coin.Code()], configuration.AbsoluteKeypath().ToUInt32(),
			messages.BTCPubRequest_ADDRESS, msgScriptType, true)
	case *eth.Coin:
		msgCoin, ok := ethMsgCoinMap[coin.Code()]
		if !ok {
			return errp.New("unsupported coin")
		}
		// No contract address, displays 'Ethereum' etc. depending on `msgCoin`.
		contractAddress := []byte{}
		if specificCoin.ERC20Token() != nil {
			// Displays the erc20 unit based on the contract.
			contractAddress = specificCoin.ERC20Token().ContractAddress().Bytes()
		}
		_, err := keystore.device.ETHPub(
			msgCoin, configuration.AbsoluteKeypath().ToUInt32(),
			messages.ETHPubRequest_ADDRESS, true, contractAddress)
		if err != nil {
			return err
		}
	default:
		return errp.New("unsupported coin")
	}
	return err
}

// CanVerifyExtendedPublicKey implements keystore.Keystore.
func (keystore *keystore) CanVerifyExtendedPublicKey() bool {
	return true
}

func (keystore *keystore) VerifyExtendedPublicKey(
	coin coinpkg.Coin, keyPath signing.AbsoluteKeypath, configuration *signing.Configuration) error {
	if !keystore.CanVerifyExtendedPublicKey() {
		panic("CanVerifyExtendedPublicKey must be true")
	}
	switch specificCoin := coin.(type) {
	case *btc.Coin:
		msgCoin, ok := btcMsgCoinMap[coin.Code()]
		if !ok {
			return errp.New("unsupported coin")
		}
		var msgOutputType messages.BTCPubRequest_OutputType
		switch specificCoin.Net().Net {
		case chaincfg.MainNetParams.Net, ltc.MainNetParams.Net:
			msgOutputTypes := map[signing.ScriptType]messages.BTCPubRequest_OutputType{
				signing.ScriptTypeP2PKH:      messages.BTCPubRequest_XPUB,
				signing.ScriptTypeP2WPKHP2SH: messages.BTCPubRequest_YPUB,
				signing.ScriptTypeP2WPKH:     messages.BTCPubRequest_ZPUB,
			}
			msgOutputType, ok = msgOutputTypes[configuration.ScriptType()]
			if !ok {
				msgOutputType = messages.BTCPubRequest_XPUB
			}
		case chaincfg.TestNet3Params.Net, ltc.TestNet4Params.Net:
			msgOutputType = messages.BTCPubRequest_TPUB
		default:
			msgOutputType = messages.BTCPubRequest_XPUB
		}
		_, err := keystore.device.BTCPub(
			msgCoin, keyPath.ToUInt32(), msgOutputType, messages.BTCScriptType_SCRIPT_UNKNOWN, true)
		if err != nil {
			return err
		}
	case *eth.Coin:
		return errp.New("unsupported operation")
	}
	return nil
}

// ExtendedPublicKey implements keystore.Keystore.
func (keystore *keystore) ExtendedPublicKey(
	coin coinpkg.Coin, keyPath signing.AbsoluteKeypath) (*hdkeychain.ExtendedKey, error) {
	switch coin.(type) {
	case *btc.Coin:
		msgCoin, ok := btcMsgCoinMap[coin.Code()]
		if !ok {
			return nil, errp.New("unsupported coin")
		}
		xpubStr, err := keystore.device.BTCPub(
			msgCoin, keyPath.ToUInt32(),
			messages.BTCPubRequest_XPUB, messages.BTCScriptType_SCRIPT_UNKNOWN, false)
		if err != nil {
			return nil, err
		}
		return hdkeychain.NewKeyFromString(xpubStr)
	case *eth.Coin:
		msgCoin, ok := ethMsgCoinMap[coin.Code()]
		if !ok {
			return nil, errp.New("unsupported coin")
		}
		xpubStr, err := keystore.device.ETHPub(
			msgCoin, keyPath.ToUInt32(), messages.ETHPubRequest_XPUB, false, []byte{})
		if err != nil {
			return nil, err
		}
		return hdkeychain.NewKeyFromString(xpubStr)
	default:
		return nil, errp.New("unsupported coin")
	}
}

func (keystore *keystore) signBTCTransaction(btcProposedTx *btc.ProposedTransaction) error {
	tx := btcProposedTx.TXProposal.Transaction

	scriptType := btcProposedTx.TXProposal.AccountConfiguration.ScriptType()
	msgScriptType, ok := btcMsgScriptTypeMap[scriptType]
	if !ok {
		return errp.Newf("Unsupported script type %s", scriptType)
	}
	coin := btcProposedTx.TXProposal.Coin.(*btc.Coin)
	msgCoin, ok := btcMsgCoinMap[coin.Code()]
	if !ok {
		return errp.Newf("coin not supported: %s", coin.Code())
	}

	// account #0
	// TODO: check that all inputs and change are the same account, and use that one.
	bip44Account := uint32(hdkeychain.HardenedKeyStart)

	inputs := make([]*messages.BTCSignInputRequest, len(tx.TxIn))
	for inputIndex, txIn := range tx.TxIn {
		prevOut := btcProposedTx.PreviousOutputs[txIn.PreviousOutPoint]
		inputs[inputIndex] = &messages.BTCSignInputRequest{
			PrevOutHash:  txIn.PreviousOutPoint.Hash[:],
			PrevOutIndex: txIn.PreviousOutPoint.Index,
			PrevOutValue: uint64(prevOut.Value),
			Sequence:     txIn.Sequence,
			Keypath: btcProposedTx.GetAddress(prevOut.ScriptHashHex()).
				Configuration.AbsoluteKeypath().ToUInt32(),
		}
	}
	outputs := make([]*messages.BTCSignOutputRequest, len(tx.TxOut))
	for index, txOut := range tx.TxOut {
		scriptClass, addresses, _, err := txscript.ExtractPkScriptAddrs(txOut.PkScript, coin.Net())
		if err != nil {
			return errp.WithStack(err)
		}
		if len(addresses) != 1 {
			return errp.New("couldn't parse pkScript")
		}
		msgOutputType, ok := btcMsgOutputTypeMap[scriptClass]
		if !ok {
			return errp.Newf("unsupported output type: %d", scriptClass)
		}
		changeAddress := btcProposedTx.TXProposal.ChangeAddress
		isChange := changeAddress != nil && bytes.Equal(
			changeAddress.PubkeyScript(),
			txOut.PkScript,
		)
		var keypath []uint32
		if isChange {
			keypath = changeAddress.Configuration.AbsoluteKeypath().ToUInt32()
		}
		outputs[index] = &messages.BTCSignOutputRequest{
			Ours:    isChange,
			Type:    msgOutputType,
			Value:   uint64(txOut.Value),
			Hash:    addresses[0].ScriptAddress(),
			Keypath: keypath,
		}
	}

	signatures, err := keystore.device.BTCSign(
		msgCoin,
		msgScriptType,
		bip44Account,
		inputs,
		outputs,
		uint32(tx.Version),
		tx.LockTime,
	)
	if firmware.IsErrorAbort(err) {
		return errp.WithStack(keystorePkg.ErrSigningAborted)
	}
	if err != nil {
		return err
	}
	for index, signature := range signatures {
		btcProposedTx.Signatures[index][keystore.CosignerIndex()] = &btcec.Signature{
			R: big.NewInt(0).SetBytes(signature[:32]),
			S: big.NewInt(0).SetBytes(signature[32:]),
		}
	}
	return nil
}

func (keystore *keystore) signETHTransaction(txProposal *eth.TxProposal) error {
	msgCoin, ok := ethMsgCoinMap[txProposal.Coin.Code()]
	if !ok {
		return errp.New("unsupported coin")
	}
	tx := txProposal.Tx
	recipient := tx.To()
	if recipient == nil {
		return errp.New("contract creation not supported")
	}
	signature, err := keystore.device.ETHSign(
		msgCoin,
		txProposal.Keypath.ToUInt32(),
		tx.Nonce(),
		tx.GasPrice(),
		tx.Gas(),
		*recipient,
		tx.Value(),
		tx.Data(),
	)
	if firmware.IsErrorAbort(err) {
		return errp.WithStack(keystorePkg.ErrSigningAborted)
	}
	if err != nil {
		return err
	}
	signedTx, err := txProposal.Tx.WithSignature(txProposal.Signer, signature)
	if err != nil {
		return err
	}
	txProposal.Tx = signedTx
	return nil
}

// SignTransaction implements keystore.Keystore.
func (keystore *keystore) SignTransaction(proposedTx interface{}) error {
	switch specificProposedTx := proposedTx.(type) {
	case *btc.ProposedTransaction:
		return keystore.signBTCTransaction(specificProposedTx)
	case *eth.TxProposal:
		return keystore.signETHTransaction(specificProposedTx)
	default:
		panic("unknown proposal type")
	}
}
