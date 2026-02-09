package token

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// --- Mock EthClient ---

type mockEthClient struct {
	codeAt       map[common.Address][]byte
	codeAtErr    error
	callResults  map[string][]byte // key: address+selector hex
	callErr      error
	callErrMap   map[string]error // per-call errors
}

func newMockEthClient() *mockEthClient {
	return &mockEthClient{
		codeAt:      make(map[common.Address][]byte),
		callResults: make(map[string][]byte),
		callErrMap:  make(map[string]error),
	}
}

func (m *mockEthClient) CodeAt(_ context.Context, contract common.Address, _ interface{}) ([]byte, error) {
	if m.codeAtErr != nil {
		return nil, m.codeAtErr
	}
	code, ok := m.codeAt[contract]
	if !ok {
		return nil, nil
	}
	return code, nil
}

func (m *mockEthClient) CallContract(_ context.Context, call ethereum.CallMsg, _ interface{}) ([]byte, error) {
	if m.callErr != nil {
		return nil, m.callErr
	}
	key := fmt.Sprintf("%s:%x", call.To.Hex(), call.Data[:4])
	if err, ok := m.callErrMap[key]; ok {
		return nil, err
	}
	if result, ok := m.callResults[key]; ok {
		return result, nil
	}
	return nil, fmt.Errorf("no mock result for %s", key)
}

// setSupportsInterface configures the mock to respond to ERC-165 supportsInterface calls
func (m *mockEthClient) setSupportsInterface(addr common.Address, interfaceID string, supported bool) {
	selectorBytes, _ := hex.DecodeString("01ffc9a7")
	key := fmt.Sprintf("%s:%x", addr.Hex(), selectorBytes)

	result := make([]byte, 32)
	if supported {
		result[31] = 1
	}
	m.callResults[key] = result
}

// setCallResult sets a mock result for a specific contract call
func (m *mockEthClient) setCallResult(addr common.Address, selector string, result []byte) {
	selectorBytes, _ := hex.DecodeString(strings.TrimPrefix(selector, "0x"))
	key := fmt.Sprintf("%s:%x", addr.Hex(), selectorBytes)
	m.callResults[key] = result
}

// setCallError sets a mock error for a specific contract call
func (m *mockEthClient) setCallError(addr common.Address, selector string, err error) {
	selectorBytes, _ := hex.DecodeString(strings.TrimPrefix(selector, "0x"))
	key := fmt.Sprintf("%s:%x", addr.Hex(), selectorBytes)
	m.callErrMap[key] = err
}

// --- Mock Storage ---

type mockTokenStorage struct {
	tokens    map[common.Address]*TokenMetadata
	saveErr   error
	getErr    error
	deleteErr error
}

func newMockTokenStorage() *mockTokenStorage {
	return &mockTokenStorage{
		tokens: make(map[common.Address]*TokenMetadata),
	}
}

func (m *mockTokenStorage) GetTokenMetadata(_ context.Context, address common.Address) (*TokenMetadata, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	meta, ok := m.tokens[address]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return meta, nil
}

func (m *mockTokenStorage) SaveTokenMetadata(_ context.Context, metadata *TokenMetadata) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.tokens[metadata.Address] = metadata
	return nil
}

func (m *mockTokenStorage) DeleteTokenMetadata(_ context.Context, address common.Address) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.tokens, address)
	return nil
}

func (m *mockTokenStorage) ListTokensByStandard(_ context.Context, standard TokenStandard, limit, offset int) ([]*TokenMetadata, error) {
	var result []*TokenMetadata
	for _, meta := range m.tokens {
		if standard == "" || meta.Standard == standard {
			result = append(result, meta)
		}
	}
	if offset >= len(result) {
		return nil, nil
	}
	end := offset + limit
	if end > len(result) {
		end = len(result)
	}
	return result[offset:end], nil
}

func (m *mockTokenStorage) GetTokensCount(_ context.Context, standard TokenStandard) (int, error) {
	count := 0
	for _, meta := range m.tokens {
		if standard == "" || meta.Standard == standard {
			count++
		}
	}
	return count, nil
}

func (m *mockTokenStorage) SearchTokens(_ context.Context, query string, limit int) ([]*TokenMetadata, error) {
	var result []*TokenMetadata
	for _, meta := range m.tokens {
		if strings.Contains(strings.ToLower(meta.Name), strings.ToLower(query)) ||
			strings.Contains(strings.ToLower(meta.Symbol), strings.ToLower(query)) {
			result = append(result, meta)
		}
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

// --- ABI Encoding helpers ---

// abiEncodeString creates ABI-encoded string data
func abiEncodeString(s string) []byte {
	// offset (32 bytes) + length (32 bytes) + data (padded to 32)
	offset := make([]byte, 32)
	offset[31] = 0x20 // offset = 32

	length := make([]byte, 32)
	length[31] = byte(len(s))

	data := make([]byte, ((len(s)+31)/32)*32)
	copy(data, []byte(s))

	result := make([]byte, 0, 64+len(data))
	result = append(result, offset...)
	result = append(result, length...)
	result = append(result, data...)
	return result
}

// abiEncodeUint8 creates ABI-encoded uint8 data
func abiEncodeUint8(v uint8) []byte {
	result := make([]byte, 32)
	result[31] = v
	return result
}

// abiEncodeUint256 creates ABI-encoded uint256 data
func abiEncodeUint256(v *big.Int) []byte {
	result := make([]byte, 32)
	b := v.Bytes()
	copy(result[32-len(b):], b)
	return result
}

// --- TokenMetadata helper method tests ---

func TestTokenMetadata_IsERC20(t *testing.T) {
	assert.True(t, (&TokenMetadata{Standard: StandardERC20}).IsERC20())
	assert.False(t, (&TokenMetadata{Standard: StandardERC721}).IsERC20())
}

func TestTokenMetadata_IsERC721(t *testing.T) {
	assert.True(t, (&TokenMetadata{Standard: StandardERC721}).IsERC721())
	assert.False(t, (&TokenMetadata{Standard: StandardERC20}).IsERC721())
}

func TestTokenMetadata_IsERC1155(t *testing.T) {
	assert.True(t, (&TokenMetadata{Standard: StandardERC1155}).IsERC1155())
	assert.False(t, (&TokenMetadata{Standard: StandardERC20}).IsERC1155())
}

func TestTokenMetadata_IsNFT(t *testing.T) {
	assert.True(t, (&TokenMetadata{Standard: StandardERC721}).IsNFT())
	assert.True(t, (&TokenMetadata{Standard: StandardERC1155}).IsNFT())
	assert.False(t, (&TokenMetadata{Standard: StandardERC20}).IsNFT())
}

func TestTokenMetadata_IsFungible(t *testing.T) {
	assert.True(t, (&TokenMetadata{Standard: StandardERC20}).IsFungible())
	assert.False(t, (&TokenMetadata{Standard: StandardERC721}).IsFungible())
}

func TestMetadataResult_HasErrors(t *testing.T) {
	assert.False(t, (&MetadataResult{Errors: map[string]error{}}).HasErrors())
	assert.True(t, (&MetadataResult{Errors: map[string]error{"name": errors.New("fail")}}).HasErrors())
}

// --- Detector tests ---

func TestNewDetector(t *testing.T) {
	d := NewDetector(newMockEthClient(), nil)
	assert.NotNil(t, d)
}

func TestDetector_DetectStandard_NoCode(t *testing.T) {
	mc := newMockEthClient()
	d := NewDetector(mc, zap.NewNop())

	addr := common.HexToAddress("0x1234")
	result := d.DetectStandard(context.Background(), addr)
	assert.Equal(t, StandardUnknown, result.Standard)
	assert.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "no code")
}

func TestDetector_DetectStandard_CodeAtError(t *testing.T) {
	mc := newMockEthClient()
	mc.codeAtErr = errors.New("rpc error")
	d := NewDetector(mc, zap.NewNop())

	result := d.DetectStandard(context.Background(), common.HexToAddress("0x1234"))
	assert.Equal(t, StandardUnknown, result.Standard)
	assert.Error(t, result.Error)
}

func TestDetector_DetectStandard_ERC1155_ViaERC165(t *testing.T) {
	mc := newMockEthClient()
	addr := common.HexToAddress("0xerc1155")
	mc.codeAt[addr] = []byte{0x60, 0x80} // has code

	// ERC-165 supportsInterface returns true for all ERC-165 calls
	mc.setSupportsInterface(addr, InterfaceIDERC165, true)

	d := NewDetector(mc, zap.NewNop())
	result := d.DetectStandard(context.Background(), addr)

	assert.Equal(t, StandardERC1155, result.Standard)
	assert.Equal(t, 1.0, result.Confidence)
	assert.True(t, result.SupportsERC165)
}

func TestDetector_DetectStandard_ERC721_ViaERC165(t *testing.T) {
	mc := newMockEthClient()
	addr := common.HexToAddress("0xerc721")
	mc.codeAt[addr] = []byte{0x60, 0x80}

	// Returns true for ERC-165 but not ERC-1155
	trueResult := make([]byte, 32)
	trueResult[31] = 1
	falseResult := make([]byte, 32)

	selectorBytes, _ := hex.DecodeString("01ffc9a7")
	key := fmt.Sprintf("%s:%x", addr.Hex(), selectorBytes)

	// The mock always returns from the same key since selector is same.
	// Need to use callErrMap for ERC-1155 check to fail, and set default to true.
	// Actually, supportsInterface always sends the same selector (01ffc9a7),
	// so we can't differentiate by key. Let's use a custom mock approach.

	// Build a more intelligent mock that checks the full calldata
	smartClient := &smartMockEthClient{
		codeAt: map[common.Address][]byte{addr: {0x60, 0x80}},
		interfaceSupport: map[string]bool{
			InterfaceIDERC165:          true,
			InterfaceIDERC721:          true,
			InterfaceIDERC721Metadata:  true,
			InterfaceIDERC1155:         false,
		},
	}

	d := NewDetector(smartClient, zap.NewNop())
	result := d.DetectStandard(context.Background(), addr)

	assert.Equal(t, StandardERC721, result.Standard)
	assert.Equal(t, 1.0, result.Confidence)
	assert.True(t, result.SupportsERC165)
	assert.True(t, result.SupportsMetadata)

	_ = key
	_ = trueResult
	_ = falseResult
}

func TestDetector_DetectStandard_ERC20_ViaBytecode(t *testing.T) {
	mc := newMockEthClient()
	addr := common.HexToAddress("0xerc20")

	// Build bytecode containing ERC-20 function selectors
	selectors := []string{
		SelectorTransfer,    // transfer
		SelectorBalanceOf,   // balanceOf
		SelectorTotalSupply, // totalSupply
		SelectorName,        // name
		SelectorSymbol,      // symbol
		SelectorDecimals,    // decimals
	}
	code := buildBytecodeWithSelectors(selectors)
	mc.codeAt[addr] = code

	// supportsInterface call fails (no ERC-165)
	mc.callErr = nil // no global error

	d := NewDetector(mc, zap.NewNop())
	result := d.DetectStandard(context.Background(), addr)

	assert.Equal(t, StandardERC20, result.Standard)
	assert.Equal(t, 0.8, result.Confidence)
	assert.True(t, result.SupportsMetadata) // has name+symbol+decimals
}

func TestDetector_DetectStandard_ERC721_ViaBytecode(t *testing.T) {
	mc := newMockEthClient()
	addr := common.HexToAddress("0xerc721bc")

	selectors := []string{
		SelectorOwnerOf,      // ownerOf - ERC721 specific
		SelectorBalanceOf,    // balanceOf
		SelectorTransferFrom, // transferFrom
		SelectorTokenURI,     // tokenURI
	}
	code := buildBytecodeWithSelectors(selectors)
	mc.codeAt[addr] = code

	d := NewDetector(mc, zap.NewNop())
	result := d.DetectStandard(context.Background(), addr)

	assert.Equal(t, StandardERC721, result.Standard)
	assert.Equal(t, 0.8, result.Confidence)
	assert.True(t, result.SupportsMetadata)
}

func TestDetector_DetectStandard_ERC1155_ViaBytecode(t *testing.T) {
	mc := newMockEthClient()
	addr := common.HexToAddress("0xerc1155bc")

	selectors := []string{
		SelectorURI,       // uri
		SelectorBalanceOf, // balanceOf
	}
	code := buildBytecodeWithSelectors(selectors)
	mc.codeAt[addr] = code

	d := NewDetector(mc, zap.NewNop())
	result := d.DetectStandard(context.Background(), addr)

	assert.Equal(t, StandardERC1155, result.Standard)
	assert.Equal(t, 0.7, result.Confidence)
}

func TestDetector_DetectStandard_Unknown(t *testing.T) {
	mc := newMockEthClient()
	addr := common.HexToAddress("0xunknown")
	mc.codeAt[addr] = []byte{0x60, 0x80, 0x60, 0x40} // generic code, no selectors

	d := NewDetector(mc, zap.NewNop())
	result := d.DetectStandard(context.Background(), addr)

	assert.Equal(t, StandardUnknown, result.Standard)
	assert.Equal(t, 0.0, result.Confidence)
	assert.NoError(t, result.Error)
}

func TestDetector_IsTokenContract_True(t *testing.T) {
	mc := newMockEthClient()
	addr := common.HexToAddress("0xtoken")
	selectors := []string{SelectorTransfer, SelectorBalanceOf}
	mc.codeAt[addr] = buildBytecodeWithSelectors(selectors)

	d := NewDetector(mc, zap.NewNop())
	assert.True(t, d.IsTokenContract(context.Background(), addr))
}

func TestDetector_IsTokenContract_False(t *testing.T) {
	mc := newMockEthClient()
	addr := common.HexToAddress("0xnottoken")
	mc.codeAt[addr] = []byte{0x60, 0x80}

	d := NewDetector(mc, zap.NewNop())
	assert.False(t, d.IsTokenContract(context.Background(), addr))
}

func TestDetector_IsTokenContract_NoCode(t *testing.T) {
	mc := newMockEthClient()
	d := NewDetector(mc, zap.NewNop())
	assert.False(t, d.IsTokenContract(context.Background(), common.HexToAddress("0xempty")))
}

// --- MetadataFetcher tests ---

func TestNewMetadataFetcher(t *testing.T) {
	f := NewMetadataFetcher(newMockEthClient(), nil)
	assert.NotNil(t, f)
}

func TestMetadataFetcher_FetchERC20Metadata(t *testing.T) {
	mc := newMockEthClient()
	addr := common.HexToAddress("0xerc20token")

	mc.setCallResult(addr, SelectorName, abiEncodeString("TestToken"))
	mc.setCallResult(addr, SelectorSymbol, abiEncodeString("TT"))
	mc.setCallResult(addr, SelectorDecimals, abiEncodeUint8(18))
	mc.setCallResult(addr, SelectorTotalSupply, abiEncodeUint256(big.NewInt(1000000)))

	f := NewMetadataFetcher(mc, zap.NewNop())
	result := f.FetchERC20Metadata(context.Background(), addr)

	assert.Equal(t, "TestToken", result.Name)
	assert.Equal(t, "TT", result.Symbol)
	assert.Equal(t, uint8(18), result.Decimals)
	assert.Equal(t, big.NewInt(1000000), result.TotalSupply)
	assert.False(t, result.HasErrors())
}

func TestMetadataFetcher_FetchERC20Metadata_PartialFailure(t *testing.T) {
	mc := newMockEthClient()
	addr := common.HexToAddress("0xerc20partial")

	mc.setCallResult(addr, SelectorName, abiEncodeString("TestToken"))
	mc.setCallResult(addr, SelectorSymbol, abiEncodeString("TT"))
	mc.setCallError(addr, SelectorDecimals, errors.New("call failed"))
	mc.setCallError(addr, SelectorTotalSupply, errors.New("call failed"))

	f := NewMetadataFetcher(mc, zap.NewNop())
	result := f.FetchERC20Metadata(context.Background(), addr)

	assert.Equal(t, "TestToken", result.Name)
	assert.Equal(t, uint8(18), result.Decimals) // defaults to 18
	assert.Nil(t, result.TotalSupply)
	assert.True(t, result.HasErrors())
	assert.Contains(t, result.Errors, "decimals")
	assert.Contains(t, result.Errors, "totalSupply")
}

func TestMetadataFetcher_FetchERC721Metadata(t *testing.T) {
	mc := newMockEthClient()
	addr := common.HexToAddress("0xerc721token")

	mc.setCallResult(addr, SelectorName, abiEncodeString("TestNFT"))
	mc.setCallResult(addr, SelectorSymbol, abiEncodeString("TNFT"))
	// tokenURI calls use different selector+data, but our mock matches by first 4 bytes
	mc.setCallResult(addr, SelectorTokenURI, abiEncodeString("https://example.com/tokens/1"))

	f := NewMetadataFetcher(mc, zap.NewNop())
	result := f.FetchERC721Metadata(context.Background(), addr)

	assert.Equal(t, "TestNFT", result.Name)
	assert.Equal(t, "TNFT", result.Symbol)
	assert.Equal(t, uint8(0), result.Decimals) // NFT has no decimals
	assert.Equal(t, "https://example.com/tokens/", result.BaseURI)
}

func TestMetadataFetcher_FetchERC1155Metadata(t *testing.T) {
	mc := newMockEthClient()
	addr := common.HexToAddress("0xerc1155token")

	// ERC-1155 name/symbol are optional
	mc.setCallError(addr, SelectorName, errors.New("not supported"))
	mc.setCallError(addr, SelectorSymbol, errors.New("not supported"))
	mc.setCallResult(addr, SelectorURI, abiEncodeString("https://example.com/metadata/{id}.json"))

	f := NewMetadataFetcher(mc, zap.NewNop())
	result := f.FetchERC1155Metadata(context.Background(), addr)

	assert.Equal(t, "", result.Name)
	assert.Equal(t, uint8(0), result.Decimals)
	assert.Equal(t, "https://example.com/metadata/{id}.json", result.BaseURI)
}

func TestMetadataFetcher_FetchMetadata_Dispatch(t *testing.T) {
	mc := newMockEthClient()
	addr := common.HexToAddress("0xdispatch")
	mc.setCallResult(addr, SelectorName, abiEncodeString("Token"))
	mc.setCallResult(addr, SelectorSymbol, abiEncodeString("TKN"))
	mc.setCallResult(addr, SelectorDecimals, abiEncodeUint8(6))
	mc.setCallResult(addr, SelectorTotalSupply, abiEncodeUint256(big.NewInt(100)))

	f := NewMetadataFetcher(mc, zap.NewNop())

	result := f.FetchMetadata(context.Background(), addr, StandardERC20)
	assert.Equal(t, "Token", result.Name)
	assert.Equal(t, uint8(6), result.Decimals)
}

func TestMetadataFetcher_FetchMetadata_UnknownStandard(t *testing.T) {
	mc := newMockEthClient()
	f := NewMetadataFetcher(mc, zap.NewNop())

	result := f.FetchMetadata(context.Background(), common.HexToAddress("0x0"), StandardUnknown)
	assert.True(t, result.HasErrors())
	assert.Contains(t, result.Errors, "standard")
}

func TestExtractBaseURI(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://example.com/tokens/123", "https://example.com/tokens/"},
		{"ipfs://QmHash/1", "ipfs://QmHash/"},
		{"noslash", "noslash"},
		{"", ""},
	}

	for _, tt := range tests {
		result := extractBaseURI(tt.input)
		assert.Equal(t, tt.expected, result, "input: %s", tt.input)
	}
}

// --- Service tests ---

func TestNewService(t *testing.T) {
	s := NewService(newMockEthClient(), newMockTokenStorage(), nil)
	assert.NotNil(t, s)
}

func TestService_DetectAndFetch_ERC20(t *testing.T) {
	client := &smartMockEthClient{
		codeAt: map[common.Address][]byte{
			common.HexToAddress("0xtoken"): buildBytecodeWithSelectors([]string{
				SelectorTransfer, SelectorBalanceOf, SelectorTotalSupply,
				SelectorName, SelectorSymbol, SelectorDecimals,
			}),
		},
		callResults: make(map[string][]byte),
	}

	addr := common.HexToAddress("0xtoken")
	client.callResults[callKey(addr, SelectorName)] = abiEncodeString("MyToken")
	client.callResults[callKey(addr, SelectorSymbol)] = abiEncodeString("MT")
	client.callResults[callKey(addr, SelectorDecimals)] = abiEncodeUint8(18)
	client.callResults[callKey(addr, SelectorTotalSupply)] = abiEncodeUint256(big.NewInt(1000))

	s := NewService(client, newMockTokenStorage(), zap.NewNop())
	meta, err := s.DetectAndFetch(context.Background(), addr, 100)
	require.NoError(t, err)
	require.NotNil(t, meta)

	assert.Equal(t, "MyToken", meta.Name)
	assert.Equal(t, "MT", meta.Symbol)
	assert.Equal(t, StandardERC20, meta.Standard)
	assert.Equal(t, uint64(100), meta.DetectedAt)
}

func TestService_DetectAndFetch_NoCode(t *testing.T) {
	client := &smartMockEthClient{
		codeAt:      make(map[common.Address][]byte),
		callResults: make(map[string][]byte),
	}

	s := NewService(client, newMockTokenStorage(), zap.NewNop())
	meta, err := s.DetectAndFetch(context.Background(), common.HexToAddress("0xempty"), 100)
	assert.Error(t, err)
	assert.Nil(t, meta)
}

func TestService_DetectAndFetch_Unknown(t *testing.T) {
	client := &smartMockEthClient{
		codeAt: map[common.Address][]byte{
			common.HexToAddress("0xgeneric"): {0x60, 0x80},
		},
		callResults: make(map[string][]byte),
	}

	s := NewService(client, newMockTokenStorage(), zap.NewNop())
	meta, err := s.DetectAndFetch(context.Background(), common.HexToAddress("0xgeneric"), 100)
	assert.NoError(t, err)
	assert.Nil(t, meta) // unknown standard returns nil
}

func TestService_IndexToken_New(t *testing.T) {
	addr := common.HexToAddress("0xnewtoken")
	client := &smartMockEthClient{
		codeAt: map[common.Address][]byte{
			addr: buildBytecodeWithSelectors([]string{
				SelectorTransfer, SelectorBalanceOf, SelectorTotalSupply,
			}),
		},
		callResults: map[string][]byte{
			callKey(addr, SelectorName):        abiEncodeString("New"),
			callKey(addr, SelectorSymbol):       abiEncodeString("NEW"),
			callKey(addr, SelectorDecimals):     abiEncodeUint8(18),
			callKey(addr, SelectorTotalSupply):  abiEncodeUint256(big.NewInt(500)),
		},
	}

	stor := newMockTokenStorage()
	s := NewService(client, stor, zap.NewNop())

	meta, err := s.IndexToken(context.Background(), addr, 50)
	require.NoError(t, err)
	require.NotNil(t, meta)
	assert.Equal(t, StandardERC20, meta.Standard)

	// Should be stored
	stored, err := stor.GetTokenMetadata(context.Background(), addr)
	require.NoError(t, err)
	assert.Equal(t, meta.Name, stored.Name)
}

func TestService_IndexToken_AlreadyExists(t *testing.T) {
	addr := common.HexToAddress("0xexisting")
	existing := &TokenMetadata{
		Address:  addr,
		Standard: StandardERC20,
		Name:     "Existing",
	}

	stor := newMockTokenStorage()
	stor.tokens[addr] = existing

	s := NewService(newMockEthClient(), stor, zap.NewNop())
	meta, err := s.IndexToken(context.Background(), addr, 100)
	require.NoError(t, err)
	assert.Equal(t, "Existing", meta.Name) // returns existing
}

func TestService_IndexToken_SaveError(t *testing.T) {
	addr := common.HexToAddress("0xsavefail")
	client := &smartMockEthClient{
		codeAt: map[common.Address][]byte{
			addr: buildBytecodeWithSelectors([]string{
				SelectorTransfer, SelectorBalanceOf, SelectorTotalSupply,
			}),
		},
		callResults: map[string][]byte{
			callKey(addr, SelectorName):        abiEncodeString("Fail"),
			callKey(addr, SelectorSymbol):       abiEncodeString("FAIL"),
			callKey(addr, SelectorDecimals):     abiEncodeUint8(18),
			callKey(addr, SelectorTotalSupply):  abiEncodeUint256(big.NewInt(0)),
		},
	}

	stor := newMockTokenStorage()
	stor.saveErr = errors.New("save failed")

	s := NewService(client, stor, zap.NewNop())
	meta, err := s.IndexToken(context.Background(), addr, 50)
	assert.Error(t, err)
	assert.Nil(t, meta)
}

func TestService_IsTokenContract(t *testing.T) {
	mc := newMockEthClient()
	addr := common.HexToAddress("0xistoken")
	mc.codeAt[addr] = buildBytecodeWithSelectors([]string{SelectorTransfer, SelectorBalanceOf})

	s := NewService(mc, newMockTokenStorage(), zap.NewNop())
	assert.True(t, s.IsTokenContract(context.Background(), addr))
}

func TestService_GetTokenMetadata(t *testing.T) {
	addr := common.HexToAddress("0xget")
	stor := newMockTokenStorage()
	stor.tokens[addr] = &TokenMetadata{Address: addr, Name: "Get"}

	s := NewService(newMockEthClient(), stor, zap.NewNop())
	meta, err := s.GetTokenMetadata(context.Background(), addr)
	require.NoError(t, err)
	assert.Equal(t, "Get", meta.Name)
}

func TestService_ListTokens(t *testing.T) {
	stor := newMockTokenStorage()
	stor.tokens[common.HexToAddress("0x01")] = &TokenMetadata{Address: common.HexToAddress("0x01"), Standard: StandardERC20}
	stor.tokens[common.HexToAddress("0x02")] = &TokenMetadata{Address: common.HexToAddress("0x02"), Standard: StandardERC721}

	s := NewService(newMockEthClient(), stor, zap.NewNop())
	tokens, err := s.ListTokens(context.Background(), StandardERC20, 10, 0)
	require.NoError(t, err)
	assert.Len(t, tokens, 1)
}

func TestService_SearchTokens(t *testing.T) {
	stor := newMockTokenStorage()
	stor.tokens[common.HexToAddress("0x01")] = &TokenMetadata{
		Address: common.HexToAddress("0x01"), Name: "StableCoin", Symbol: "SC",
	}

	s := NewService(newMockEthClient(), stor, zap.NewNop())
	tokens, err := s.SearchTokens(context.Background(), "stable", 10)
	require.NoError(t, err)
	assert.Len(t, tokens, 1)
}

func TestService_GetTokensCount(t *testing.T) {
	stor := newMockTokenStorage()
	stor.tokens[common.HexToAddress("0x01")] = &TokenMetadata{Standard: StandardERC20}
	stor.tokens[common.HexToAddress("0x02")] = &TokenMetadata{Standard: StandardERC20}

	s := NewService(newMockEthClient(), stor, zap.NewNop())
	count, err := s.GetTokensCount(context.Background(), StandardERC20)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

// --- TokenIndexerAdapter tests ---

func TestNewTokenIndexerAdapter(t *testing.T) {
	s := NewService(newMockEthClient(), newMockTokenStorage(), nil)
	adapter := NewTokenIndexerAdapter(s)
	assert.NotNil(t, adapter)
}

// --- StorageTokenMetadataFetcher tests ---

func TestNewStorageTokenMetadataFetcher(t *testing.T) {
	f := NewStorageTokenMetadataFetcher(newMockEthClient(), nil)
	assert.NotNil(t, f)
}

// --- convertStandardToStorage tests ---

func TestConvertStandardToStorage(t *testing.T) {
	tests := []struct {
		input    TokenStandard
		expected string
	}{
		{StandardERC20, "ERC20"},
		{StandardERC721, "ERC721"},
		{StandardERC1155, "ERC1155"},
		{StandardUnknown, "UNKNOWN"},
	}

	for _, tt := range tests {
		result := convertStandardToStorage(tt.input)
		assert.Equal(t, tt.expected, string(result))
	}
}

// --- Constants tests ---

func TestConstants_InterfaceIDs(t *testing.T) {
	assert.Equal(t, "0x01ffc9a7", InterfaceIDERC165)
	assert.Equal(t, "0x80ac58cd", InterfaceIDERC721)
	assert.Equal(t, "0xd9b67a26", InterfaceIDERC1155)
}

func TestConstants_FunctionSelectors(t *testing.T) {
	assert.Equal(t, "0xa9059cbb", SelectorTransfer)
	assert.Equal(t, "0x70a08231", SelectorBalanceOf)
	assert.Equal(t, "0x18160ddd", SelectorTotalSupply)
}

// --- Smart mock client ---

type smartMockEthClient struct {
	codeAt           map[common.Address][]byte
	codeAtErr        error
	interfaceSupport map[string]bool // interfaceID -> supported
	callResults      map[string][]byte
	callErrMap       map[string]error
}

func (m *smartMockEthClient) CodeAt(_ context.Context, contract common.Address, _ interface{}) ([]byte, error) {
	if m.codeAtErr != nil {
		return nil, m.codeAtErr
	}
	return m.codeAt[contract], nil
}

func (m *smartMockEthClient) CallContract(_ context.Context, call ethereum.CallMsg, _ interface{}) ([]byte, error) {
	if call.To == nil {
		return nil, errors.New("no target")
	}
	dataHex := hex.EncodeToString(call.Data)

	// Check if this is a supportsInterface call (01ffc9a7)
	if len(call.Data) >= 8 && dataHex[:8] == "01ffc9a7" {
		// Extract interface ID from calldata bytes 4-8
		interfaceIDHex := "0x" + dataHex[8:16]
		if m.interfaceSupport != nil {
			if supported, ok := m.interfaceSupport[interfaceIDHex]; ok {
				result := make([]byte, 32)
				if supported {
					result[31] = 1
				}
				return result, nil
			}
		}
		return make([]byte, 32), nil // false by default
	}

	// Check specific call results
	key := callKey(*call.To, "0x"+dataHex[:8])
	if m.callErrMap != nil {
		if err, ok := m.callErrMap[key]; ok {
			return nil, err
		}
	}
	if m.callResults != nil {
		if result, ok := m.callResults[key]; ok {
			return result, nil
		}
	}

	return nil, fmt.Errorf("no mock for call %s", key)
}

func callKey(addr common.Address, selector string) string {
	selectorBytes, _ := hex.DecodeString(strings.TrimPrefix(selector, "0x"))
	return fmt.Sprintf("%s:%x", addr.Hex(), selectorBytes)
}

// buildBytecodeWithSelectors creates fake bytecode that contains the given function selectors
func buildBytecodeWithSelectors(selectors []string) []byte {
	// Start with some EVM bytecode prefix
	code := []byte{0x60, 0x80, 0x60, 0x40, 0x52}
	for _, sel := range selectors {
		selBytes, _ := hex.DecodeString(strings.TrimPrefix(sel, "0x"))
		code = append(code, selBytes...)
		code = append(code, 0x00, 0x00) // padding
	}
	return code
}
