package itest

import (
	"crypto/rand"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/lightninglabs/taproot-assets/internal/test"
	"github.com/lightninglabs/taproot-assets/rfqmsg"
	"github.com/lightningnetwork/lnd/chainreg"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lntest"
	"github.com/lightningnetwork/lnd/lntest/node"
	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/stretchr/testify/require"
)

func testRfqQuoteRequest(t *harnessTest) {
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
		42, 10,
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

func testRfqHtlcIntercept(t *harnessTest) {
	ht := t.lndHarness

	// Initialize the test context with 3 connected nodes.
	ts := newInterceptorTestScenario(ht)

	alice, bob, carol := ts.alice, ts.bob, ts.carol

	// Set up a tapd node for Bob. (The primary tapd node should be assigned
	// to Alice.)
	bobTapd := setupTapdHarness(t.t, t, bob, t.universeServer)
	defer func() {
		require.NoError(t.t, bobTapd.stop(!*noDelete))
	}()

	// Open and wait for channels.
	const chanAmt = btcutil.Amount(300000)
	p := lntest.OpenChannelParams{Amt: chanAmt}
	reqs := []*lntest.OpenChannelRequest{
		{Local: alice, Remote: bob, Param: p},
		{Local: bob, Remote: carol, Param: p},
	}
	resp := ht.OpenMultiChannelsAsync(reqs)
	cpAB, cpBC := resp[0], resp[1]

	// Make sure Alice is aware of channel Bob=>Carol.
	ht.AssertTopologyChannelOpen(alice, cpBC)

	// Prepare the test cases.
	req := &lnrpc.Invoice{ValueMsat: 1000}
	addResponse := carol.RPC.AddInvoice(req)
	invoice := carol.RPC.LookupInvoice(addResponse.RHash)
	tc := &interceptorTestCase{
		amountMsat: 1000,
		invoice:    invoice,
		payAddr:    invoice.PaymentAddr,
	}

	// We initiate a payment from Alice to Carol via Bob.
	ts.sendPaymentAndAssertAction(tc)

	// Finally, close channels.
	ht.CloseChannel(alice, cpAB)
	ht.CloseChannel(bob, cpBC)
}

//func testRfqHtlcInterceptBak(t *harnessTest) {
//	var (
//		alice = t.lndHarness.Alice
//		bob   = t.lndHarness.Bob
//	)
//
//	// Set up a tapd node for Bob. (The primary tapd node should be assigned
//	// to Alice.)
//	bobTapd := setupTapdHarness(
//		t.t, t, t.lndHarness.Bob, t.universeServer,
//	)
//	defer func() {
//		require.NoError(t.t, bobTapd.stop(!*noDelete))
//	}()
//
//	// Ensure that there's a communication connection between Alice and Bob.
//	t.lndHarness.EnsureConnected(alice, bob)
//
//	// Open a channel with outbound capacity from Alice to Bob.
//	chanPoint := t.lndHarness.OpenChannel(
//		alice, bob, lntest.OpenChannelParams{Amt: 300000},
//	)
//
//	channels := alice.RPC.ListChannels(&lnrpc.ListChannelsRequest{})
//	t.Logf("Alice channels: %v", channels)
//	t.Logf("Alice chanPoint: %v", chanPoint)
//
//	// Setup LND node Charlie.
//	charlie := newLndNode(t.lndHarness, "Charlie")
//
//	// Ensure that there's a communication connection between existing nodes
//	// and Charlie.
//	t.lndHarness.EnsureConnected(alice, charlie)
//	t.lndHarness.EnsureConnected(bob, charlie)
//
//	// Open a channel with outbound capacity from Bob to Charlie.
//	chanPoint = t.lndHarness.OpenChannel(
//		bob, charlie, lntest.OpenChannelParams{Amt: 300000},
//	)
//	defer t.lndHarness.CloseChannel(bob, chanPoint)
//
//	// Ensure that the graph has been synced between all nodes.
//	t.lndHarness.WaitForGraphSync(alice)
//	t.lndHarness.WaitForGraphSync(bob)
//	t.lndHarness.WaitForGraphSync(charlie)
//
//	t.lndHarness.EnsureConnected(alice, bob)
//	t.lndHarness.EnsureConnected(alice, charlie)
//	t.lndHarness.EnsureConnected(bob, charlie)
//
//	// Generate an invoice with node Charlie. This invoice will be paid by
//	// Alice. The payment process will include routing a HTLC via Bob.
//	t.Log("Creating invoice with node Charlie")
//	preimage := test.RandBytes(32)
//
//	paymentAmt := btcutil.Amount(10000)
//	invoice := &lnrpc.Invoice{
//		Memo:      "invoice_from_charlie",
//		RPreimage: preimage,
//		Value:     int64(paymentAmt),
//	}
//	addInvoiceResp := charlie.RPC.AddInvoice(invoice)
//
//	// Subscribe the invoice.
//	invoiceStatus := charlie.RPC.SubscribeSingleInvoice(
//		addInvoiceResp.RHash,
//	)
//
//	// Alice pays Charlie's invoice.
//	t.Log("Alice pays Charlie's invoice.")
//	t.lndHarness.CompletePaymentRequests(
//		alice, []string{addInvoiceResp.PaymentRequest},
//	)
//
//	// Charlie waits until the invoice is settled.
//	t.Log("Waiting for Charlie's invoice to be settled.")
//	t.lndHarness.AssertInvoiceState(invoiceStatus, lnrpc.Invoice_SETTLED)
//}

//func newLndNode(lndHarness *lntest.HarnessTest,
//	nodeName string) *node.HarnessNode {
//
//	h := lndHarness
//
//	lndArgs := []string{
//		"--default-remote-max-htlcs=483",
//		"--dust-threshold=5000000",
//	}
//	newNode := h.NewNode(nodeName, lndArgs)
//
//	addrReq := &lnrpc.NewAddressRequest{
//		Type: lnrpc.AddressType_WITNESS_PUBKEY_HASH,
//	}
//
//	const (
//		initialFund         = 1 * btcutil.SatoshiPerBitcoin
//		totalTxes           = 100
//		defaultMinerFeeRate = 7500
//		numBlocksSendOutput = 2
//	)
//
//	// Load up the wallets of the seeder node with 100 outputs of 1 BTC
//	// each.
//	for i := 0; i < totalTxes; i++ {
//		resp := newNode.RPC.NewAddress(addrReq)
//
//		addr, err := btcutil.DecodeAddress(
//			resp.Address, h.Miner.ActiveNet,
//		)
//		require.NoError(h, err)
//
//		addrScript, err := txscript.PayToAddrScript(addr)
//		require.NoError(h, err)
//
//		output := &wire.TxOut{
//			PkScript: addrScript,
//			Value:    initialFund,
//		}
//		h.Miner.SendOutput(output, defaultMinerFeeRate)
//	}
//
//	// We generate several blocks in order to give the outputs created
//	// above a good number of confirmations.
//	h.MineBlocksAndAssertNumTxes(numBlocksSendOutput, totalTxes)
//
//	// Now we want to wait for the nodes to catch up.
//	h.WaitForBlockchainSync(newNode)
//
//	// Now block until both wallets have fully synced up.
//	const expectedBalance = 100 * initialFund
//	err := wait.NoError(func() error {
//		aliceResp := newNode.RPC.WalletBalance()
//
//		if aliceResp.ConfirmedBalance != expectedBalance {
//			return fmt.Errorf("expected 10 BTC, instead "+
//				"alice has %d", aliceResp.ConfirmedBalance)
//		}
//
//		return nil
//	}, wait.DefaultTimeout)
//	require.NoError(h, err, "timeout checking balance for node")
//
//	return newNode
//}

type interceptorTestCase struct {
	amountMsat        int64
	payAddr           []byte
	invoice           *lnrpc.Invoice
	shouldHold        bool
	interceptorAction routerrpc.ResolveHoldForwardAction
}

// interceptorTestScenario is a helper struct to hold the test context and
// provide the needed functionality.
type interceptorTestScenario struct {
	ht                *lntest.HarnessTest
	alice, bob, carol *node.HarnessNode
}

// newInterceptorTestScenario initializes a new test scenario with three nodes
// and connects them to have the following topology,
//
//	Alice --> Bob --> Carol
//
// Among them, Alice and Bob are standby nodes and Carol is a new node.
func newInterceptorTestScenario(
	ht *lntest.HarnessTest) *interceptorTestScenario {

	alice, bob := ht.Alice, ht.Bob
	carol := ht.NewNode("carol", nil)

	ht.EnsureConnected(alice, bob)
	ht.EnsureConnected(bob, carol)

	return &interceptorTestScenario{
		ht:    ht,
		alice: alice,
		bob:   bob,
		carol: carol,
	}
}

// prepareTestCases prepares 4 tests:
// 1. failed htlc.
// 2. resumed htlc.
// 3. settling htlc externally.
// 4. held htlc that is resumed later.
func (c *interceptorTestScenario) prepareTestCases() []*interceptorTestCase {
	var (
		actionFail   = routerrpc.ResolveHoldForwardAction_FAIL
		actionResume = routerrpc.ResolveHoldForwardAction_RESUME
		actionSettle = routerrpc.ResolveHoldForwardAction_SETTLE
	)

	cases := []*interceptorTestCase{
		{
			amountMsat: 1000, shouldHold: false,
			interceptorAction: actionFail,
		},
		{
			amountMsat: 1000, shouldHold: false,
			interceptorAction: actionResume,
		},
		{
			amountMsat: 1000, shouldHold: false,
			interceptorAction: actionSettle,
		},
		{
			amountMsat: 1000, shouldHold: true,
			interceptorAction: actionResume,
		},
	}

	for _, t := range cases {
		inv := &lnrpc.Invoice{ValueMsat: t.amountMsat}
		addResponse := c.carol.RPC.AddInvoice(inv)
		invoice := c.carol.RPC.LookupInvoice(addResponse.RHash)

		// We'll need to also decode the returned invoice so we can
		// grab the payment address which is now required for ALL
		// payments.
		payReq := c.carol.RPC.DecodePayReq(invoice.PaymentRequest)

		t.invoice = invoice
		t.payAddr = payReq.PaymentAddr
	}

	return cases
}

var (
	customTestKey   uint64 = 394829
	customTestValue        = []byte{1, 3, 5}
)

// sendPaymentAndAssertAction sends a payment from alice to carol and asserts
// that the specified interceptor action is taken.
func (c *interceptorTestScenario) sendPaymentAndAssertAction(
	tc *interceptorTestCase) *lnrpc.HTLCAttempt {

	// Build a route from alice to carol.
	route := c.buildRoute(
		tc.amountMsat, []*node.HarnessNode{c.bob, c.carol}, tc.payAddr,
	)

	// Send a custom record to the forwarding node.
	route.Hops[0].CustomRecords = map[uint64][]byte{
		customTestKey: customTestValue,
	}

	// Send the payment.
	sendReq := &routerrpc.SendToRouteRequest{
		PaymentHash: tc.invoice.RHash,
		Route:       route,
	}

	return c.alice.RPC.SendToRouteV2(sendReq)
}

func (c *interceptorTestScenario) assertAction(tc *interceptorTestCase,
	attempt *lnrpc.HTLCAttempt) {

	// Now check the expected action has been taken.
	switch tc.interceptorAction {
	// For 'fail' interceptor action we make sure the payment failed.
	case routerrpc.ResolveHoldForwardAction_FAIL:
		require.Equal(c.ht, lnrpc.HTLCAttempt_FAILED, attempt.Status,
			"expected payment to fail")

		// Assert that we get a temporary channel failure which has a
		// channel update.
		require.NotNil(c.ht, attempt.Failure)
		require.NotNil(c.ht, attempt.Failure.ChannelUpdate)

		require.Equal(c.ht, lnrpc.Failure_TEMPORARY_CHANNEL_FAILURE,
			attempt.Failure.Code)

	// For settle and resume we make sure the payment is successful.
	case routerrpc.ResolveHoldForwardAction_SETTLE:
		fallthrough

	case routerrpc.ResolveHoldForwardAction_RESUME:
		require.Equal(c.ht, lnrpc.HTLCAttempt_SUCCEEDED,
			attempt.Status, "expected payment to succeed")
	}
}

// buildRoute is a helper function to build a route with given hops.
func (c *interceptorTestScenario) buildRoute(amtMsat int64,
	hops []*node.HarnessNode, payAddr []byte) *lnrpc.Route {

	rpcHops := make([][]byte, 0, len(hops))
	for _, hop := range hops {
		k := hop.PubKeyStr
		pubkey, err := route.NewVertexFromStr(k)
		require.NoErrorf(c.ht, err, "error parsing %v: %v", k, err)
		rpcHops = append(rpcHops, pubkey[:])
	}

	req := &routerrpc.BuildRouteRequest{
		AmtMsat:        amtMsat,
		FinalCltvDelta: chainreg.DefaultBitcoinTimeLockDelta,
		HopPubkeys:     rpcHops,
		PaymentAddr:    payAddr,
	}

	routeResp := c.alice.RPC.BuildRoute(req)

	return routeResp.Route
}
