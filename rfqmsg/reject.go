package rfqmsg

import (
	"bytes"
	"fmt"
	"io"

	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/lightningnetwork/lnd/tlv"
)

const (
	// Reject message type field TLV types.

	TypeRejectID      tlv.Type = 0
	TypeRejectErrCode tlv.Type = 1
	TypeRejectErrMsg  tlv.Type = 3
)

func TypeRecordRejectID(id *ID) tlv.Record {
	const recordSize = 32

	return tlv.MakeStaticRecord(
		TypeRejectID, id, recordSize, IdEncoder, IdDecoder,
	)
}

func TypeRecordRejectErrCode(errCode *uint8) tlv.Record {
	return tlv.MakePrimitiveRecord(TypeRejectErrCode, errCode)
}

func TypeRecordRejectErrMsg(errMsg *string) tlv.Record {
	sizeFunc := func() uint64 {
		return uint64(len(*errMsg))
	}
	return tlv.MakeDynamicRecord(
		TypeRejectErrMsg, errMsg, sizeFunc,
		rejectErrMsgEncoder, rejectErrMsgDecoder,
	)
}

func rejectErrMsgEncoder(w io.Writer, val any, buf *[8]byte) error {
	if typ, ok := val.(**string); ok {
		v := *typ

		msgBytes := []byte(*v)
		err := tlv.EVarBytes(w, msgBytes, buf)
		if err != nil {
			return err
		}

		return nil
	}

	return tlv.NewTypeForEncodingErr(val, "RejectErrMsg")
}

func rejectErrMsgDecoder(r io.Reader, val any, buf *[8]byte, l uint64) error {
	if typ, ok := val.(**string); ok {
		var errMsgBytes []byte
		err := tlv.DVarBytes(r, &errMsgBytes, buf, l)
		if err != nil {
			return err
		}

		msgStr := string(errMsgBytes)
		*typ = &msgStr

		return nil
	}

	return tlv.NewTypeForDecodingErr(val, "RejectErrMsg", l, l)
}

// RejectErr is a struct that represents the error code and message of a quote
// reject message.
type RejectErr struct {
	// Code is the error code that provides the reason for the rejection.
	Code uint8

	// Msg is the error message that provides the reason for the rejection.
	Msg string
}

var (
	// ErrUnknownReject is the error code for when the quote is rejected
	// for an unspecified reason.
	ErrUnknownReject = RejectErr{
		Code: 0,
		Msg:  "unknown reject error",
	}

	// ErrNoSuitableSellOffer is the error code for when there is no
	// suitable sell offer available.
	ErrNoSuitableSellOffer = RejectErr{
		Code: 1,
		Msg:  "no suitable sell offer available",
	}

	// ErrPriceOracleUnavailable is the error code for when the price
	// oracle is unavailable.
	ErrPriceOracleUnavailable = RejectErr{
		Code: 2,
		Msg:  "price oracle service unavailable",
	}

	// ErrPriceOracleUnspecifiedError is the error code for when the price
	// oracle returns an unspecified error.
	ErrPriceOracleUnspecifiedError = RejectErr{
		Code: 3,
		Msg:  "price oracle service returned an unspecified error",
	}

	// ErrPriceOracleError is the error code for when the price oracle
	// returns a specified error.
	ErrPriceOracleError = RejectErr{
		Code: 4,
		Msg:  "price oracle service error: (err_code=%d, err_msg=%s)",
	}
)

// NewErrPriceOracleError creates a new instance of a reject message price
// oracle error.
func NewErrPriceOracleError(oracleErrCode uint8,
	oracleErrMsg string) RejectErr {

	// Sanitise price oracle error message by truncating to 255 characters.
	// The price oracle service might be a third-party service and could
	// return an error message that is too long.
	if len(oracleErrMsg) > 255 {
		oracleErrMsg = oracleErrMsg[:255]
	}

	errMsg := fmt.Sprintf(
		ErrPriceOracleError.Msg, oracleErrCode, oracleErrMsg,
	)
	return RejectErr{
		Code: ErrPriceOracleError.Code,
		Msg:  errMsg,
	}
}

// rejectMsgData is a struct that represents the data field of a quote
// reject message.
type rejectMsgData struct {
	// ID represents the unique identifier of the quote request message that
	// this response is associated with.
	ID ID

	// Err is the error code and message that provides the reason for the
	// rejection.
	Err RejectErr
}

// EncodeRecords determines the non-nil records to include when encoding at
// runtime.
func (q *rejectMsgData) encodeRecords() []tlv.Record {
	return []tlv.Record{
		TypeRecordRejectID(&q.ID),
		TypeRecordRejectErrCode(&q.Err.Code),
		TypeRecordRejectErrMsg(&q.Err.Msg),
	}
}

// Encode encodes the structure into a TLV stream.
func (q *rejectMsgData) Encode(writer io.Writer) error {
	stream, err := tlv.NewStream(q.encodeRecords()...)
	if err != nil {
		return err
	}
	return stream.Encode(writer)
}

// DecodeRecords provides all TLV records for decoding.
func (q *rejectMsgData) decodeRecords() []tlv.Record {
	return []tlv.Record{
		TypeRecordRejectID(&q.ID),
		TypeRecordRejectErrCode(&q.Err.Code),
		TypeRecordRejectErrMsg(&q.Err.Msg),
	}
}

// Decode decodes the structure from a TLV stream.
func (q *rejectMsgData) Decode(r io.Reader) error {
	stream, err := tlv.NewStream(q.decodeRecords()...)
	if err != nil {
		return err
	}
	return stream.Decode(r)
}

// Bytes encodes the structure into a TLV stream and returns the bytes.
func (q *rejectMsgData) Bytes() ([]byte, error) {
	var b bytes.Buffer
	err := q.Encode(&b)
	if err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

// Reject is a struct that represents a quote reject message.
type Reject struct {
	// Peer is the peer that sent the quote request.
	Peer route.Vertex

	// rejectMsgData is the message data for the quote reject message.
	rejectMsgData
}

// NewReject creates a new instance of a quote reject message.
func NewReject(request Request, rejectErr RejectErr) *Reject {
	return &Reject{
		Peer: request.Peer,
		rejectMsgData: rejectMsgData{
			ID:  request.ID,
			Err: rejectErr,
		},
	}
}

// NewQuoteRejectFromWireMsg instantiates a new instance from a wire message.
func NewQuoteRejectFromWireMsg(wireMsg WireMessage) (*Reject, error) {
	// Decode message data component from TLV bytes.
	var msgData rejectMsgData
	err := msgData.Decode(bytes.NewReader(wireMsg.Data))
	if err != nil {
		return nil, fmt.Errorf("unable to decode quote reject "+
			"message data: %w", err)
	}

	return &Reject{
		Peer:          wireMsg.Peer,
		rejectMsgData: msgData,
	}, nil
}

// ToWire returns a wire message with a serialized data field.
func (q *Reject) ToWire() (WireMessage, error) {
	// Encode message data component as TLV bytes.
	msgDataBytes, err := q.rejectMsgData.Bytes()
	if err != nil {
		return WireMessage{}, fmt.Errorf("unable to encode message "+
			"data: %w", err)
	}

	return WireMessage{
		Peer:    q.Peer,
		MsgType: MsgTypeReject,
		Data:    msgDataBytes,
	}, nil
}

func (q *Reject) MsgPeer() route.Vertex {
	return q.Peer
}

// Ensure that the message type implements the OutgoingMsg interface.
var _ OutgoingMsg = (*Reject)(nil)

// Ensure that the message type implements the IncomingMsg interface.
var _ IncomingMsg = (*Reject)(nil)
