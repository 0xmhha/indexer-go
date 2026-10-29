package arcingress

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/stretchr/testify/require"
	"testing"
)

func protocolMeta() importMeta {
	return importMeta{Version: 1, RequestID: 1, ChainID: 5042002, Genesis: execwire.Hash{1}, ArcRevision: ArcRevision, RethRevision: RethRevision, ImportEpoch: 1, Height: 1, Hash: execwire.Hash{2}, Parent: execwire.Hash{1}, ParentRoot: execwire.Hash{3}}
}
func TestNativeInputExactFramingAndDigest(t *testing.T) {
	m := protocolMeta()
	block, receipts := []byte{0xc1, 0xc0}, []byte{0xc0}
	frame, digest, err := encodeInput(m, block, receipts)
	require.NoError(t, err)
	meta, _ := json.Marshal(m)
	var expected bytes.Buffer
	expected.WriteString("AIN1")
	for _, n := range []int{len(meta), len(block), len(receipts)} {
		require.NoError(t, binary.Write(&expected, binary.BigEndian, uint32(n)))
	}
	expected.Write(meta)
	expected.Write(block)
	expected.Write(receipts)
	require.Equal(t, expected.Bytes(), frame)
	require.Equal(t, execwire.Hash(sha256.Sum256(frame)), digest)
	m.RequestID = 0
	_, _, err = encodeInput(m, block, receipts)
	require.Error(t, err)
}
func protocolReply(magic string, id uint64, p []byte) []byte {
	var b bytes.Buffer
	b.WriteString(magic)
	_ = binary.Write(&b, binary.BigEndian, id)
	_ = binary.Write(&b, binary.BigEndian, uint32(len(p)))
	b.Write(p)
	return b.Bytes()
}
func TestNativeReplyStrictBoundedCanonicalAndExactID(t *testing.T) {
	valid := []byte(`{"Height":1,"Hash":"0x0100000000000000000000000000000000000000000000000000000000000000","Root":"0x0200000000000000000000000000000000000000000000000000000000000000"}`)
	var ref nativeRef
	require.NoError(t, readReply(bytes.NewReader(protocolReply("AWR1", 7, valid)), "AWR1", 7, &ref))
	require.Equal(t, uint64(1), ref.Height)
	for _, p := range [][]byte{append(bytes.Clone(valid), ' '), bytes.Replace(valid, []byte(`"Height":1`), []byte(`"Height":1,"Height":1`), 1), bytes.Replace(valid, []byte(`"Height":1`), []byte(`"Unknown":1`), 1), append(bytes.Clone(valid), valid...), valid[:len(valid)-1]} {
		require.Error(t, readReply(bytes.NewReader(protocolReply("AWR1", 7, p)), "AWR1", 7, &nativeRef{}))
	}
	require.Error(t, readReply(bytes.NewReader(protocolReply("AWR1", 8, valid)), "AWR1", 7, &nativeRef{}))
	require.Error(t, readReply(bytes.NewReader(protocolReply("AWH1", 7, valid)), "AWR1", 7, &nativeRef{}))
	header := protocolReply("AWR1", 7, nil)
	binary.BigEndian.PutUint32(header[12:], maxMeta+1)
	require.Error(t, readReply(bytes.NewReader(header), "AWR1", 7, &nativeRef{}))
}
