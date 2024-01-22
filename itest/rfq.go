package itest

import (
	"math/rand"
	"time"

	"github.com/lightninglabs/taproot-assets/internal/test"
	rfqmsg "github.com/lightninglabs/taproot-assets/rfqmessages"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/stretchr/testify/require"
)

func testQuoteRequest(t *harnessTest) {
	// Ensure Alice and Bob are connected.
	t.lndHarness.EnsureConnected(t.lndHarness.Alice, t.lndHarness.Bob)

	// Generate a random quote request id.
	var randomQuoteRequestId [32]byte
	_, err := rand.Read(randomQuoteRequestId[:])
	require.NoError(t.t, err, "unable to generate random quote request id")

	//// Generate a random asset id.
	//var randomAssetId asset.ID
	//_, err = rand.Read(randomAssetId[:])
	//require.NoError(t.t, err, "unable to generate random asset id")

	// Generate a random asset group key.
	randomGroupPrivateKey := test.RandPrivKey(t.t)

	quoteRequestMsgData, err := rfqmsg.NewQuoteRequestMsgData(
		randomQuoteRequestId, nil, randomGroupPrivateKey.PubKey(), 42,
		10,
	)

	// TLV encode the quote request.
	quoteReqBytes, err := quoteRequestMsgData.Bytes()
	require.NoError(t.t, err, "unable to encode quote request")

	resAlice := t.lndHarness.Alice.RPC.GetInfo()
	t.Logf("Sending custom message to alias: %s", resAlice.Alias)

	t.lndHarness.Bob.RPC.SendCustomMessage(&lnrpc.SendCustomMessageRequest{
		Peer: t.lndHarness.Alice.PubKey[:],
		Type: rfqmsg.MsgTypeQuoteRequest,
		Data: quoteReqBytes,
	})

	// Wait for Alice to receive the quote request.
	time.Sleep(5 * time.Second)
}
