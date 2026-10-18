package capture

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/triedb/database"
)

type immutableNodes struct {
	tx     port.CaptureReader
	prefix string
}

func immutableNodeKey(prefix string, h common.Hash) []byte {
	return []byte(fmt.Sprintf("%s/nodes/%x", prefix, h[:]))
}
func (d immutableNodes) NodeReader(common.Hash) (database.NodeReader, error) { return d, nil }
func (d immutableNodes) Node(owner common.Hash, _ []byte, h common.Hash) ([]byte, error) {
	if owner != (common.Hash{}) {
		return nil, ErrConflict
	}
	if reads, ok := d.tx.(*immutableReads); ok {
		return reads.node(d.prefix, h)
	}
	b, e := d.tx.Get(immutableNodeKey(d.prefix, h))
	if e != nil {
		return nil, errors.Join(ErrConflict, e)
	}
	if !validImmutableNode(b, h) {
		return nil, ErrConflict
	}
	return b, nil
}
func immutableTrie(tx port.CaptureReader, root common.Hash, prefix string) (*trie.Trie, error) {
	t, e := trie.New(trie.TrieID(root), immutableNodes{tx, prefix})
	if e != nil {
		return nil, errors.Join(ErrConflict, e)
	}
	return t, nil
}

// Nodes remain immutable so retained and historical roots stay readable.
func storeImmutableNodes(tx port.CaptureWriter, prefix string, nodes *trienode.NodeSet) (err error) {
	if nodes == nil {
		return nil
	}
	nodes.ForEachWithOrder(func(_ string, n *trienode.Node) {
		if err != nil || n.IsDeleted() {
			return
		}
		if len(n.Blob) > 1<<20 || crypto.Keccak256Hash(n.Blob) != n.Hash {
			err = ErrConflict
			return
		}
		key := immutableNodeKey(prefix, n.Hash)
		prior, e := tx.Get(key)
		if e != nil && !errors.Is(e, port.ErrNotFound) {
			err = e
			return
		}
		if e == nil && !bytes.Equal(prior, n.Blob) {
			err = ErrConflict
			return
		}
		err = tx.Put(key, n.Blob)
	})
	return err
}
