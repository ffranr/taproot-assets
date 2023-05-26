package mssmt

import (
	"bytes"
	"encoding/hex"
	"math"
	"math/rand"
	"testing"

	"github.com/lightninglabs/taproot-assets/internal/test"
	"github.com/stretchr/testify/require"
)

// RandLeafAmount generates a random leaf node sum amount.
func RandLeafAmount() uint64 {
	minSum := uint64(1)
	maxSum := uint64(math.MaxUint32)
	return (rand.Uint64() % maxSum) + minSum
}

func ParseProof(t testing.TB, proofHex string) Proof {
	t.Helper()

	proofBytes, err := hex.DecodeString(proofHex)
	require.NoError(t, err)

	var compressedProof CompressedProof
	err = compressedProof.Decode(bytes.NewReader(proofBytes))
	require.NoError(t, err)

	proof, err := compressedProof.Decompress()
	require.NoError(t, err)

	return *proof
}

func NewTestFromNode(t testing.TB, node Node) *TestNode {
	t.Helper()

	nodeHash := node.NodeHash()
	return &TestNode{
		Hash: hex.EncodeToString(nodeHash[:]),
		Sum:  node.NodeSum(),
	}
}

type TestNode struct {
	Hash string `json:"hash"`
	Sum  uint64 `json:"sum"`
}

func (tn *TestNode) ToNode(t testing.TB) ComputedNode {
	t.Helper()

	return NewComputedNode(test.Parse32Byte(t, tn.Hash), tn.Sum)
}
