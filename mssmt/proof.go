package mssmt

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/lightninglabs/taproot-assets/chanutils"
)

var (
	// ErrInvalidCompressedProof is returned when a compressed proof has an
	// invalid combination of explicit nodes and default hash bits.
	ErrInvalidCompressedProof = errors.New("mssmt: invalid compressed proof")
)

// Proof represents a merkle proof for a MS-SMT.
type Proof struct {
	// Nodes represents the siblings that should be hashed with the leaf and
	// its parents to arrive at the root of the MS-SMT.
	Nodes []Node
}

// MarshalJSON implements the json.Marshaler interface.
//
// NOTE: This implementation is necessary because the Nodes field in its
// uncompressed form contains too many elements. Here we return a map of the
// non-empty nodes keyed on their index in the proof.
func (p *Proof) MarshalJSON() ([]byte, error) {
	compressedProof := p.Compress()

	nodes := make(map[int]Node)
	nodeIdx := 0

	for idx, _ := range compressedProof.Bits {
		isEmpty := compressedProof.Bits[idx]
		if !isEmpty {
			nodes[idx] = compressedProof.Nodes[nodeIdx]
			nodeIdx++
		}
	}

	// Marshal the map to JSON
	return json.Marshal(nodes)
}

// UnmarshalJSON implements the json.Unmarshaler interface.
//
// NOTE: This function effectively reverses the marshalling done in
// MarshalJSON. See that function for more details.
func (p *Proof) UnmarshalJSON(data []byte) error {
	nodes := make(map[int]Node)
	err := json.Unmarshal(data, &nodes)
	if err != nil {
		return err
	}

	baseNodes := EmptyTree[:MaxTreeLevels]
	for idx, node := range nodes {
		baseNodes[idx] = node
	}

	p.Nodes = baseNodes
	return nil
}

// CompressedProof represents a compressed MS-SMT merkle proof. Since merkle
// proofs for a MS-SMT are always constant size (255 nodes), we replace its
// empty nodes by a bit vector.
type CompressedProof struct {
	// Bits determines whether a sibling node within a proof is part of the
	// empty tree. This allows us to efficiently compress proofs by not
	// including any pre-computed nodes.
	Bits []bool

	// Nodes represents the non-empty siblings that should be hashed with
	// the leaf and its parents to arrive at the root of the MS-SMT.
	Nodes []Node
}

// NewProof initializes a new merkle proof for the given leaf node.
func NewProof(nodes []Node) *Proof {
	return &Proof{
		Nodes: nodes,
	}
}

// Root returns the root node obtained by walking up the tree.
func (p Proof) Root(key [32]byte, leaf *LeafNode) *BranchNode {
	// Note that we don't need to check the error here since the only point
	// where the error could come from is the passed iterator which is nil.
	node, _ := walkUp(&key, leaf, p.Nodes, nil)
	return node
}

// Copy returns a deep copy of the proof.
func (p Proof) Copy() *Proof {
	nodesCopy := make([]Node, len(p.Nodes))
	for idx := range p.Nodes {
		nodesCopy[idx] = p.Nodes[idx].Copy()
	}
	return &Proof{Nodes: nodesCopy}
}

// Compress compresses a merkle proof by replacing its empty nodes with a bit
// vector.
func (p Proof) Compress() *CompressedProof {
	var (
		bits  = make([]bool, len(p.Nodes))
		nodes []Node
	)
	for idx := range p.Nodes {
		node := p.Nodes[idx]

		// The proof nodes start at the leaf, while the EmptyTree starts
		// at the root.
		if node.NodeHash() == EmptyTree[MaxTreeLevels-idx].NodeHash() {
			bits[idx] = true
		} else {
			nodes = append(nodes, node)
		}
	}
	return &CompressedProof{
		Bits:  bits,
		Nodes: nodes,
	}
}

// Decompress decompresses a compressed merkle proof by replacing its bit vector
// with the empty nodes it represents.
func (p *CompressedProof) Decompress() (*Proof, error) {
	nextNodeIdx := 0
	nodes := make([]Node, len(p.Bits))

	// The number of 0 bits should match the number of pre-populated nodes.
	numExpectedNodes := chanutils.Reduce(p.Bits, func(count int, bit bool) int {
		if !bit {
			return count + 1
		}

		return count
	})

	if numExpectedNodes != len(p.Nodes) {
		return nil, fmt.Errorf("%w, num_nodes=%v, num_expected=%v",
			ErrInvalidCompressedProof, len(p.Nodes), numExpectedNodes)
	}

	for i, bitSet := range p.Bits {
		if bitSet {
			// The proof nodes start at the leaf, while the
			// EmptyTree starts at the root.
			nodes[i] = EmptyTree[MaxTreeLevels-i]
		} else {
			nodes[i] = p.Nodes[nextNodeIdx]
			nextNodeIdx++
		}
	}

	return NewProof(nodes), nil
}
