package itest

import (
	"crypto/rand"
	"time"

	"github.com/lightninglabs/taproot-assets/internal/test"
	"github.com/lightninglabs/taproot-assets/rfqmsg"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/routing/route"
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

	peer := route.Vertex(t.lndHarness.Alice.PubKey[:])

	quoteRequest := rfqmsg.NewRequestMsg(
		peer, randomQuoteRequestId, nil, randomGroupPrivateKey.PubKey(),
		42, 10, 4,
	)
	require.NoError(t.t, err, "unable to create quote request message data")

	wireMsg, err := quoteRequest.ToWire()
	require.NoError(t.t, err, "unable to create wire message")

	resAlice := t.lndHarness.Alice.RPC.GetInfo()
	t.Logf("Sending custom message to alias: %s", resAlice.Alias)

	t.lndHarness.Bob.RPC.SendCustomMessage(&lnrpc.SendCustomMessageRequest{
		Peer: wireMsg.Peer[:],
		Type: wireMsg.MsgType,
		Data: wireMsg.Data,
	})

	// Wait for Alice to receive the quote request.
	time.Sleep(5 * time.Second)
}
