package rfqmsg

import (
	"encoding/binary"
	"testing"

	"github.com/lightninglabs/taproot-assets/internal/test"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/stretchr/testify/require"
)

// TestAcceptShortChannelId tests the ShortChannelId method of a quote accept
// message.
func TestAcceptShortChannelId(t *testing.T) {
	t.Parallel()

	scidInt := uint64(123456)
	scid := lnwire.NewShortChanIDFromInt(scidInt)

	// Create a random ID.
	randomIdBytes := test.RandBytes(32)
	id := ID(randomIdBytes)

	// Set the last 8 bytes of the ID to the short channel ID.
	binary.BigEndian.PutUint64(id[24:], scid.ToUint64())

	// Create an accept message.
	acceptMsg := Accept{
		acceptMsgData: acceptMsgData{
			ID: id,
		},
	}

	// Derive the short channel ID from the accept message.
	actualScidInt := acceptMsg.ShortChannelId()

	// Assert that the derived short channel ID is equal to the expected
	// short channel ID.
	require.Equal(t, scidInt, uint64(actualScidInt))
}
