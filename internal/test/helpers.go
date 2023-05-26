package test

import (
	"encoding/hex"
	"encoding/json"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/lightningnetwork/lnd/keychain"
	"github.com/lightningnetwork/lnd/lnrpc/signrpc"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/constraints"
)

// RandBool rolls a random boolean.
func RandBool() bool {
	return rand.Int()%2 == 0
}

// RandInt makes a random integer of the specified type.
func RandInt[T constraints.Integer]() T {
	return T(rand.Int63()) // nolint:gosec
}

func RandOp(t testing.TB) wire.OutPoint {
	t.Helper()

	op := wire.OutPoint{
		Index: uint32(RandInt[int32]()),
	}
	_, err := rand.Read(op.Hash[:])
	require.NoError(t, err)

	return op
}

func RandPrivKey(t testing.TB) *btcec.PrivateKey {
	privKey, err := btcec.NewPrivateKey()
	require.NoError(t, err)
	return privKey
}

func SchnorrPubKey(t testing.TB, privKey *btcec.PrivateKey) *btcec.PublicKey {
	return SchnorrKey(t, privKey.PubKey())
}

func SchnorrKey(t testing.TB, pubKey *btcec.PublicKey) *btcec.PublicKey {
	key, err := schnorr.ParsePubKey(schnorr.SerializePubKey(pubKey))
	require.NoError(t, err)
	return key
}

func RandPubKey(t testing.TB) *btcec.PublicKey {
	return SchnorrPubKey(t, RandPrivKey(t))
}

func RandBytes(num int) []byte {
	randBytes := make([]byte, num)
	_, _ = rand.Read(randBytes)
	return randBytes
}

func PubToKeyDesc(p *btcec.PublicKey) keychain.KeyDescriptor {
	return keychain.KeyDescriptor{
		PubKey: p,
	}
}

func ParseRPCKeyDescriptor(t testing.TB,
	rpcDesc *signrpc.KeyDescriptor) keychain.KeyDescriptor {

	pubKey, err := btcec.ParsePubKey(rpcDesc.RawKeyBytes)
	require.NoError(t, err)

	require.NotNil(t, rpcDesc.KeyLoc)

	return keychain.KeyDescriptor{
		KeyLocator: keychain.KeyLocator{
			Family: keychain.KeyFamily(rpcDesc.KeyLoc.KeyFamily),
			Index:  uint32(rpcDesc.KeyLoc.KeyIndex),
		},
		PubKey: pubKey,
	}
}

func ParsePubKey(t testing.TB, key string) *btcec.PublicKey {
	t.Helper()

	pkBytes, err := hex.DecodeString(key)
	require.NoError(t, err)

	pk, err := btcec.ParsePubKey(pkBytes)
	require.NoError(t, err)

	return pk
}

func ParseOutPoint(t testing.TB, op string) wire.OutPoint {
	parts := strings.Split(op, ":")
	require.Len(t, parts, 2)

	hash := ParseChainHash(t, parts[0])

	outputIndex, err := strconv.Atoi(parts[1])
	require.NoError(t, err)

	return wire.OutPoint{
		Hash:  hash,
		Index: uint32(outputIndex),
	}
}

func ParseChainHash(t testing.TB, hash string) chainhash.Hash {
	require.Equal(t, chainhash.HashSize, hex.DecodedLen(len(hash)))

	h, err := chainhash.NewHashFromStr(hash)
	require.NoError(t, err)
	return *h
}

func Parse32Byte(t testing.TB, b string) [32]byte {
	t.Helper()

	require.Equal(t, hex.EncodedLen(32), len(b))

	var result [32]byte
	_, err := hex.Decode(result[:], []byte(b))
	require.NoError(t, err)

	return result
}

func Parse33Byte(t testing.TB, b string) [33]byte {
	t.Helper()

	require.Equal(t, hex.EncodedLen(33), len(b))

	var result [33]byte
	_, err := hex.Decode(result[:], []byte(b))
	require.NoError(t, err)

	return result
}

func ParseHex(t testing.TB, b string) []byte {
	t.Helper()

	result, err := hex.DecodeString(b)
	require.NoError(t, err)

	return result
}

func ParseSchnorrSig(t testing.TB, sigHex string) schnorr.Signature {
	t.Helper()

	require.Len(t, sigHex, hex.EncodedLen(schnorr.SignatureSize))

	sigBytes, err := hex.DecodeString(sigHex)
	require.NoError(t, err)

	sig, err := schnorr.ParseSignature(sigBytes)
	require.NoError(t, err)

	return *sig
}

func ParseTestVectors(t testing.TB, fileName string, target any) {
	fileBytes, err := os.ReadFile(fileName)
	require.NoError(t, err)

	err = json.Unmarshal(fileBytes, target)
	require.NoError(t, err)
}

func HexPubKey(pk *btcec.PublicKey) string {
	return hex.EncodeToString(pk.SerializeCompressed())
}

func HexSchnorrPubKey(pk *btcec.PublicKey) string {
	return hex.EncodeToString(schnorr.SerializePubKey(pk))
}

func HexSignature(sig *schnorr.Signature) string {
	return hex.EncodeToString(sig.Serialize())
}

func ComputeTaprootScript(t testing.TB, taprootKey *btcec.PublicKey) []byte {
	script, err := txscript.NewScriptBuilder().
		AddOp(txscript.OP_1).
		AddData(schnorr.SerializePubKey(taprootKey)).
		Script()
	require.NoError(t, err)
	return script
}

func RandHash() chainhash.Hash {
	var hash chainhash.Hash
	copy(hash[:], RandBytes(chainhash.HashSize))
	return hash
}

func RandTxWitnesses(t testing.TB) wire.TxWitness {
	numElements := RandInt[int]() % 5
	if numElements == 0 {
		return nil
	}

	w := make(wire.TxWitness, numElements)
	for i := 0; i < numElements; i++ {
		elem := make([]byte, 10)
		_, err := rand.Read(elem)
		require.NoError(t, err)

		w[i] = elem
	}

	return w
}

// ScriptHashLock returns a simple bitcoin script that locks the funds to a hash
// lock of the given preimage.
func ScriptHashLock(t *testing.T, preimage []byte) txscript.TapLeaf {
	builder := txscript.NewScriptBuilder()
	builder.AddOp(txscript.OP_DUP)
	builder.AddOp(txscript.OP_HASH160)
	builder.AddData(btcutil.Hash160(preimage))
	builder.AddOp(txscript.OP_EQUALVERIFY)
	script1, err := builder.Script()
	require.NoError(t, err)
	return txscript.NewBaseTapLeaf(script1)
}

// ScriptSchnorrSig returns a simple bitcoin script that locks the funds to a
// Schnorr signature of the given public key.
func ScriptSchnorrSig(t *testing.T, pubKey *btcec.PublicKey) txscript.TapLeaf {
	builder := txscript.NewScriptBuilder()
	builder.AddData(schnorr.SerializePubKey(pubKey))
	builder.AddOp(txscript.OP_CHECKSIG)
	script2, err := builder.Script()
	require.NoError(t, err)
	return txscript.NewBaseTapLeaf(script2)
}
