package cosmos

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	authv1beta1 "cosmossdk.io/api/cosmos/auth/v1beta1"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/golang/protobuf/ptypes"
	"github.com/stretchr/testify/assert"
)

const (
	fromPrivkey = ""
	toPrivkey   = ""
)

func Test_cosmos_send(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultDialTimeout)
	defer cancel()

	walletConfig, _ := getWalletConfig()
	client, err := DialCosmosClient(ctx, walletConfig)
	assert.NoError(t, err)

	from, to, err := setupTestAccounts(t)
	assert.NoError(t, err)

	fromResponse, err := client.GetAccount(from.Address.String())
	assert.NoError(t, err)

	fromAuthaccount := new(authv1beta1.BaseAccount)
	err = ptypes.UnmarshalAny(fromResponse.Account, fromAuthaccount)
	assert.NoError(t, err)
	fmt.Printf("sequence: %s, account number: %s, address: %s \n",
		strconv.FormatUint(fromAuthaccount.GetSequence(), 10),
		strconv.FormatUint(fromAuthaccount.GetAccountNumber(), 10),
		fromAuthaccount.GetAddress())

	balance, err := client.GetBalance("uatom", from.Address.String())
	assert.NoError(t, err)

	fmt.Printf("余额信息:\n")
	fmt.Printf("地址: %s\n", from.Address.String())
	fmt.Printf("余额: %s%s\n", balance.Amount, balance.Denom)

	msg := &banktypes.MsgSend{
		FromAddress: from.Address.String(),
		ToAddress:   to.Address.String(),
		Amount: sdk.NewCoins(
			sdk.NewCoin("uatom", math.NewInt(1000)),
		),
	}

	txBuilder := client.context.TxConfig.NewTxBuilder()
	err = txBuilder.SetMsgs(msg)
	assert.NoError(t, err)

	txBuilder.SetGasLimit(200000)
	txBuilder.SetFeeAmount(sdk.NewCoins(
		sdk.NewCoin("uatom", math.NewInt(2000)),
	))
	txBuilder.SetMemo("")

	status, err := client.rpchttp.Status(context.Background())
	assert.NoError(t, err)
	chainID := status.NodeInfo.Network

	fmt.Printf("\n交易信息:\n")
	fmt.Printf("Chain ID: %s\n", chainID)
	fmt.Printf("Account Number: %d\n", fromAuthaccount.GetAccountNumber())
	fmt.Printf("Sequence: %d\n", fromAuthaccount.GetSequence())
	fmt.Printf("From Address: %s\n", from.Address.String())
	fmt.Printf("To Address: %s\n", to.Address.String())
	fmt.Printf("Amount: %s\n", msg.Amount.String())
	fmt.Printf("Gas Limit: %d\n", txBuilder.GetTx().GetGas())
	fmt.Printf("Fee: %s\n", txBuilder.GetTx().GetFee().String())
	fmt.Printf("Memo: %s\n", txBuilder.GetTx().GetMemo())

	// 准备签名
	signMode := signing.SignMode_SIGN_MODE_DIRECT

	// 设置空签名以获取签名字节
	sig := signing.SignatureV2{
		PubKey: from.PubKey,
		Data: &signing.SingleSignatureData{
			SignMode:  signMode,
			Signature: nil,
		},
		Sequence: fromAuthaccount.GetSequence(),
	}

	err = txBuilder.SetSignatures(sig)
	assert.NoError(t, err)

	// 获取签名字节
	signerData := authsigning.SignerData{
		ChainID:       chainID,
		AccountNumber: fromAuthaccount.GetAccountNumber(),
		Sequence:      fromAuthaccount.GetSequence(),
	}

	bytesToSign, err := authsigning.GetSignBytesAdapter(
		context.Background(),
		client.context.TxConfig.SignModeHandler(),
		signMode,
		signerData,
		txBuilder.GetTx(),
	)
	assert.NoError(t, err)

	// 使用私钥签名
	signature, err := from.PrivKey.Sign(bytesToSign)
	assert.NoError(t, err)

	// 设置最终签名
	sig = signing.SignatureV2{
		PubKey: from.PubKey,
		Data: &signing.SingleSignatureData{
			SignMode:  signMode,
			Signature: signature,
		},
		Sequence: fromAuthaccount.GetSequence(),
	}

	err = txBuilder.SetSignatures(sig)
	assert.NoError(t, err)

	// 编码交易
	txBytes, err := client.context.TxConfig.TxEncoder()(txBuilder.GetTx())
	assert.NoError(t, err)

	// 广播交易
	res, err := client.BroadcastTx(txBytes)
	assert.NoError(t, err)

	if res.CheckTx.Code != 0 {
		t.Fatalf("交易检查失败: %s", res.CheckTx.Log)
	}
	if res.TxResult.Code != 0 {
		t.Fatalf("交易执行失败: %s", res.TxResult.Log)
	}

	fmt.Printf("\n交易成功!\n")
	fmt.Printf("交易哈希: %X\n", res.Hash)
	fmt.Printf("交易高度: %d\n", res.Height)

	// 等待交易确认后查询接收方余额
	time.Sleep(6 * time.Second)
	toBalance, err := client.GetBalance("uatom", to.Address.String())
	assert.NoError(t, err)
	fmt.Printf("\n接收方余额: %s%s\n", toBalance.Amount, toBalance.Denom)
}

// Account 结构体用于存储账户信息
type Account struct {
	PrivKey *secp256k1.PrivKey
	PubKey  cryptotypes.PubKey
	Address sdk.AccAddress
}

// setupTestAccounts 创建测试账户
func setupTestAccounts(t *testing.T) (*Account, *Account, error) {
	from, err := createNewAccountV2(fromPrivkey)
	if err != nil {
		return nil, nil, fmt.Errorf("创建发送方账户失败: %w", err)
	}

	to, err := createNewAccountV2(toPrivkey)
	if err != nil {
		return nil, nil, fmt.Errorf("创建接收方账户失败: %w", err)
	}

	return from, to, nil
}

func createNewAccountV2(privKeyHex string) (*Account, error) {
	privKey, err := FromPrivKeyHex(privKeyHex)
	if err != nil {
		return nil, fmt.Errorf("解析私钥失败: %w", err)
	}

	pubKey := privKey.PubKey()
	addr := sdk.AccAddress(pubKey.Address())

	return &Account{
		PrivKey: privKey,
		PubKey:  pubKey,
		Address: addr,
	}, nil
}
