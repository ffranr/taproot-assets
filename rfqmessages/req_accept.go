package rfqmessages

import (
	"bytes"
	"fmt"
	"io"

	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/lightningnetwork/lnd/tlv"
)

const (
	// QuoteAcceptMsgData field TLV types.

	QuoteAcceptMsgDataIDType             tlv.Type = 0
	QuoteAcceptMsgDataCharacteristicType tlv.Type = 2
	QuoteAcceptMsgDataExpiryType         tlv.Type = 4
	QuoteAcceptMsgDataSignatureType      tlv.Type = 6
)

func QuoteAcceptMsgDataIDRecord(id *ID) tlv.Record {
	return tlv.MakePrimitiveRecord(QuoteAcceptMsgDataIDType, id)
}

func QuoteAcceptMsgDataCharacteristicRecord(characteristic *uint64) tlv.Record {
	return tlv.MakePrimitiveRecord(
		QuoteAcceptMsgDataCharacteristicType, characteristic,
	)
}

func QuoteAcceptMsgDataExpiryRecord(expirySeconds *uint64) tlv.Record {
	return tlv.MakePrimitiveRecord(
		QuoteAcceptMsgDataExpiryType, expirySeconds,
	)
}

func QuoteAcceptMsgDataSig(sig *[64]byte) tlv.Record {
	return tlv.MakePrimitiveRecord(
		QuoteAcceptMsgDataSignatureType, sig,
	)
}

// QuoteAcceptMsgData is a struct that represents a request for a quote (RFQ) from a
// peer.
type QuoteAcceptMsgData struct {
	// ID is the unique identifier of the request for quote (RFQ).
	ID ID

	// AmtCharacteristic is the characteristic of the asset amount that
	// determines the fee rate.
	AmtCharacteristic uint64

	// ExpirySeconds is the number of seconds until the quote expires.
	ExpirySeconds uint64

	// Sig is a signature over the serialized contents of the message.
	Sig [64]byte
}

//func NewQuoteAcceptMsgData(q *QuoteAccept) (*QuoteAcceptMsgData, error) {
//	// Hash the fields of the message data so that we can create a signature
//	// over the message.
//	h := sha256.New()
//
//	_, err := h.Write(q.ID[:])
//	if err != nil {
//		return nil, err
//	}
//
//	err = binary.Write(h, binary.BigEndian, q.AmtCharacteristic)
//	if err != nil {
//		return nil, err
//	}
//
//	err = binary.Write(h, binary.BigEndian, q.ExpirySeconds)
//	if err != nil {
//		return nil, err
//	}
//
//	// TODO(ffranr): Sign the hash of the message data.
//	//fieldsHash := h.Sum(nil)
//	//sig
//
//	return &QuoteAcceptMsgData{
//		ID:                q.ID,
//		AmtCharacteristic: q.AmtCharacteristic,
//		ExpirySeconds:     q.ExpirySeconds,
//		//Sig:               sig,
//	}, nil
//}

// EncodeRecords determines the non-nil records to include when encoding an
// asset witness at runtime.
func (q *QuoteAcceptMsgData) encodeRecords() []tlv.Record {
	var records []tlv.Record

	// Add ID record.
	records = append(records, QuoteAcceptMsgDataIDRecord(&q.ID))

	// Add characteristic record.
	record := QuoteAcceptMsgDataCharacteristicRecord(
		&q.AmtCharacteristic,
	)
	records = append(records, record)

	// Add expiry record.
	records = append(
		records, QuoteAcceptMsgDataExpiryRecord(&q.ExpirySeconds),
	)

	// Add signature record.
	records = append(
		records, QuoteAcceptMsgDataSig(&q.Sig),
	)

	return records
}

// Encode encodes the structure into a TLV stream.
func (q *QuoteAcceptMsgData) Encode(writer io.Writer) error {
	stream, err := tlv.NewStream(q.encodeRecords()...)
	if err != nil {
		return err
	}
	return stream.Encode(writer)
}

// DecodeRecords provides all TLV records for decoding.
func (q *QuoteAcceptMsgData) decodeRecords() []tlv.Record {
	return []tlv.Record{
		QuoteAcceptMsgDataIDRecord(&q.ID),
		QuoteAcceptMsgDataCharacteristicRecord(&q.AmtCharacteristic),
		QuoteAcceptMsgDataExpiryRecord(&q.ExpirySeconds),
		QuoteAcceptMsgDataSig(&q.Sig),
	}
}

// Decode decodes the structure from a TLV stream.
func (q *QuoteAcceptMsgData) Decode(r io.Reader) error {
	stream, err := tlv.NewStream(q.decodeRecords()...)
	if err != nil {
		return err
	}
	return stream.Decode(r)
}

// QuoteAccept is a struct that represents an accepted quote message.
type QuoteAccept struct {
	// Peer is the peer that sent the quote request.
	Peer route.Vertex

	// QuoteAcceptMsgData is the message data from the quote accept message.
	QuoteAcceptMsgData
}

// LndCustomMsg returns a custom message that can be sent to a peer using the
// lndclient.
func (q *QuoteAccept) LndCustomMsg() (*lndclient.CustomMessage, error) {
	// Encode message data component as TLV bytes.
	var buff *bytes.Buffer
	err := q.QuoteAcceptMsgData.Encode(buff)
	if err != nil {
		return nil, fmt.Errorf("unable to encode quote accept "+
			"message data: %w", err)
	}
	quoteAcceptBytes := buff.Bytes()

	return &lndclient.CustomMessage{
		Peer:    q.Peer,
		MsgType: MsgTypeQuoteAccept,
		Data:    quoteAcceptBytes,
	}, nil
}
