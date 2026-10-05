package constants

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// TestEventSignaturesMatchStableNet requires every system contract event
// signature the indexer decodes to be declared by go-stablenet's system
// contracts (testdata/stablenet_events.txt, extracted from the Solidity
// sources). A wrong parameter list gives a topic no log ever carries, so the
// event would silently never be indexed. Transfer and Approval are the
// standard ERC-20 events the contracts inherit.
func TestEventSignaturesMatchStableNet(t *testing.T) {
	f, err := os.Open("testdata/stablenet_events.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	declared := map[common.Hash]string{
		crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)")): "Transfer(address,address,uint256)",
		crypto.Keccak256Hash([]byte("Approval(address,address,uint256)")): "Approval(address,address,uint256)",
	}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		sig := strings.Fields(line)[0]
		declared[crypto.Keccak256Hash([]byte(sig))] = sig
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}

	for topic, name := range EventSignatureToName {
		if _, ok := declared[topic]; !ok {
			t.Errorf("%s (topic %s) is not an event of go-stablenet's system contracts", name, topic.Hex())
		}
	}
}
