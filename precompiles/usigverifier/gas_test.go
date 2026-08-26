package usigverifier

import (
	"crypto/ed25519"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"
)

// Gas pricing for verifyEd25519RawMessage (F-2026-18140 remediation).
//
// ed25519.Verify runs a SHA-512 pass over the whole message, so its CPU cost
// grows with the message while the old price was a flat 4000 regardless of
// length (~58 µs at 32 B vs ~922 µs at 1 MB on a live node — 16x the work for
// the same fee). The remediation is twofold: price the message per 32-byte word,
// and refuse messages past MaxEd25519MessageBytes outright, because this is a
// view method a contract can loop from memory without re-paying the calldata.

// capGas is what a message of exactly MaxEd25519MessageBytes costs, and the most
// any verifyEd25519RawMessage call can ever be charged.
const capGas = VerifyEd25519RawMessageBaseGas +
	(MaxEd25519MessageBytes/32)*VerifyEd25519RawMessagePerWordGas

// rawMessageCalldata ABI-encodes a verifyEd25519RawMessage call with a message
// of msgLen bytes, exactly as the EVM would hand it to the precompile.
func rawMessageCalldata(tb testing.TB, msgLen int) []byte {
	tb.Helper()

	cd, err := ABI.Pack(
		VerifyEd25519RawMessageMethod,
		make([]byte, ed25519.PublicKeySize),
		make([]byte, msgLen),
		make([]byte, ed25519.SignatureSize),
	)
	require.NoError(tb, err)

	return cd
}

// TestRequiredGas_RawMessageScalesWithMessageLength locks in the price curve:
// a flat base plus VerifyEd25519RawMessagePerWordGas for every 32-byte word of
// the message. A flat price fails every row past the first.
func TestRequiredGas_RawMessageScalesWithMessageLength(t *testing.T) {
	p, err := NewPrecompile()
	require.NoError(t, err)

	for _, tc := range []struct {
		name   string
		msgLen int
		want   uint64
	}{
		{"empty", 0, 4000},
		{"1 byte rounds up to a word", 1, 4012},
		{"32 bytes / 1 word", 32, 4012},
		{"33 bytes / 2 words", 33, 4024},
		{"1 KiB", 1024, 4000 + 32*12},
		{"8 KiB", 8 * 1024, 4000 + 256*12},
		{"64 KiB", 64 * 1024, 4000 + 2048*12},
		{"128 KiB (cap)", MaxEd25519MessageBytes, 4000 + 4096*12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, p.RequiredGas(rawMessageCalldata(t, tc.msgLen)),
				"gas must be %d base + %d per 32-byte word",
				VerifyEd25519RawMessageBaseGas, VerifyEd25519RawMessagePerWordGas)
		})
	}
}

// TestRequiredGas_RawMessageIsStrictlyIncreasing is the property behind the
// table: every extra word of message must cost more than the word before it.
func TestRequiredGas_RawMessageIsStrictlyIncreasing(t *testing.T) {
	p, err := NewPrecompile()
	require.NoError(t, err)

	prev := uint64(0)
	for _, msgLen := range []int{0, 32, 64, 1024, 8 * 1024, 64 * 1024, MaxEd25519MessageBytes} {
		gas := p.RequiredGas(rawMessageCalldata(t, msgLen))
		require.Greater(t, gas, prev,
			"a %d-byte message must cost more than the smaller one before it", msgLen)
		prev = gas
	}

	// And the growth has to be material, not a rounding error: the largest
	// accepted message costs an order of magnitude more than the base.
	require.Greater(t, prev, 10*VerifyEd25519RawMessageBaseGas,
		"a cap-sized message must cost far more than the flat base price")
}

// TestRequiredGas_SmallMessagesKeepASaneCost guards the other direction: the
// per-word term must not make ordinary calls (a digest, a short payload)
// noticeably more expensive than they used to be.
func TestRequiredGas_SmallMessagesKeepASaneCost(t *testing.T) {
	p, err := NewPrecompile()
	require.NoError(t, err)

	require.Equal(t, VerifyEd25519RawMessageBaseGas, p.RequiredGas(rawMessageCalldata(t, 0)),
		"an empty message costs exactly the base")

	for _, msgLen := range []int{1, 32, 64, 128} {
		gas := p.RequiredGas(rawMessageCalldata(t, msgLen))
		require.Greater(t, gas, VerifyEd25519RawMessageBaseGas,
			"a %d-byte message must cost more than an empty one", msgLen)
		require.LessOrEqual(t, gas, VerifyEd25519RawMessageBaseGas+100,
			"a %d-byte message must stay within a rounding error of the old flat price", msgLen)
	}
}

// TestRequiredGas_AboveCapIsClampedNotUnbounded: a message past the cap is
// rejected by Run, but it must still be priced — at the cap's price, never more,
// so the charge cannot be inflated (or overflowed) by a declared length.
func TestRequiredGas_AboveCapIsClampedNotUnbounded(t *testing.T) {
	p, err := NewPrecompile()
	require.NoError(t, err)

	for _, tc := range []struct {
		name   string
		msgLen int
	}{
		{"one byte over the cap", MaxEd25519MessageBytes + 1},
		{"twice the cap", 2 * MaxEd25519MessageBytes},
		{"1 MiB", 1024 * 1024},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, capGas, p.RequiredGas(rawMessageCalldata(t, tc.msgLen)),
				"oversized messages must be priced at the cap, not above it")
		})
	}
}

// TestRequiredGas_LegacyMethodStaysFlat documents the deliberate divergence
// between the two constants. verifyEd25519 takes a bytes32 digest and always
// verifies the same 66-byte ASCII hex string, so its cost does not depend on the
// calldata — even an oversized pubKey argument cannot change the work done.
func TestRequiredGas_LegacyMethodStaysFlat(t *testing.T) {
	p, err := NewPrecompile()
	require.NoError(t, err)

	var digest [32]byte

	for _, pubKeyLen := range []int{32, 1024, 64 * 1024} {
		cd, err := ABI.Pack(
			VerifyEd25519Method,
			make([]byte, pubKeyLen),
			digest,
			make([]byte, ed25519.SignatureSize),
		)
		require.NoError(t, err)

		require.Equal(t, VerifyEd25519Gas, p.RequiredGas(cd),
			"verifyEd25519 verifies a fixed 66-byte message, so it stays flat (pubKey len=%d)", pubKeyLen)
	}
}

// TestRequiredGas_MalformedCalldataIsPanicFreeAndBounded: RequiredGas runs
// before any validation, on whatever bytes the caller supplied, so it must never
// panic and must never charge more than a cap-sized message.
func TestRequiredGas_MalformedCalldataIsPanicFreeAndBounded(t *testing.T) {
	p, err := NewPrecompile()
	require.NoError(t, err)

	valid := rawMessageCalldata(t, 64)

	// mutate overwrites the 32-byte word at args[wordIdx] of a valid calldata.
	mutate := func(wordIdx int, word []byte) []byte {
		out := make([]byte, len(valid))
		copy(out, valid)
		copy(out[4+wordIdx*32:4+(wordIdx+1)*32], word)
		return out
	}

	allOnes := make([]byte, 32)
	for i := range allOnes {
		allOnes[i] = 0xff
	}

	// The `message` tail sits at args[160:] for a 32-byte pubKey: 3 head words
	// plus the pubKey tail (length word + one data word).
	const messageLenWordIdx = 5

	for _, tc := range []struct {
		name  string
		input []byte
	}{
		{"nil", nil},
		{"one byte", []byte{0x01}},
		{"selector only", valid[:4]},
		{"selector plus half a head word", valid[:4+16]},
		{"head truncated before the message offset", valid[:4+32]},
		{"tail truncated at the message length word", valid[:4+160]},
		{"message offset larger than a uint64", mutate(1, allOnes)},
		{"message offset points past the calldata", mutate(1, append(make([]byte, 31), 0xff))},
		{"message length larger than a uint64", mutate(messageLenWordIdx, allOnes)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gas uint64
			require.NotPanics(t, func() { gas = p.RequiredGas(tc.input) },
				"RequiredGas must never panic on malformed calldata")
			require.LessOrEqual(t, gas, capGas,
				"malformed calldata must never be charged more than a cap-sized message")
		})
	}

	// An absurd declared length is priced at the cap rather than at the base, so
	// lying about the size is not the cheap path.
	require.Equal(t, capGas, p.RequiredGas(mutate(messageLenWordIdx, allOnes)))
}

// TestVerifyEd25519RawMessage_RejectsOversizedMessage is the hard cap: past
// MaxEd25519MessageBytes the call reverts instead of verifying.
func TestVerifyEd25519RawMessage_RejectsOversizedMessage(t *testing.T) {
	p, err := NewPrecompile()
	require.NoError(t, err)

	priv := ed25519.NewKeyFromSeed(testSeed)
	pub := priv.Public().(ed25519.PublicKey)

	method := ABI.Methods[VerifyEd25519RawMessageMethod]
	msg := make([]byte, MaxEd25519MessageBytes+1)
	sig := ed25519.Sign(priv, msg)

	bz, err := p.VerifyEd25519RawMessage(&method, []interface{}{[]byte(pub), msg, sig})

	// Value assertion first: an error assertion aborts the test, and a nil
	// result is the thing that proves no verification happened.
	require.Nil(t, bz, "an oversized message must not produce a verification result")
	require.Error(t, err, "a message past the cap must be rejected, not verified")
	require.Contains(t, err.Error(), "message too large")
}

// TestVerifyEd25519RawMessage_AcceptsMessageAtCap: the cap is inclusive — a
// message of exactly MaxEd25519MessageBytes still verifies.
func TestVerifyEd25519RawMessage_AcceptsMessageAtCap(t *testing.T) {
	p, err := NewPrecompile()
	require.NoError(t, err)

	priv := ed25519.NewKeyFromSeed(testSeed)
	pub := priv.Public().(ed25519.PublicKey)

	method := ABI.Methods[VerifyEd25519RawMessageMethod]
	msg := make([]byte, MaxEd25519MessageBytes)
	sig := ed25519.Sign(priv, msg)

	bz, err := p.VerifyEd25519RawMessage(&method, []interface{}{[]byte(pub), msg, sig})
	require.NoError(t, err)

	out, err := method.Outputs.Unpack(bz)
	require.NoError(t, err)
	require.Equal(t, []interface{}{true}, out, "a message of exactly the cap must still verify")
}

// TestRun_OversizedMessageReverts drives the same cap through the entry point
// the EVM actually calls, on real ABI-encoded calldata.
func TestRun_OversizedMessageReverts(t *testing.T) {
	p, err := NewPrecompile()
	require.NoError(t, err)

	priv := ed25519.NewKeyFromSeed(testSeed)
	pub := priv.Public().(ed25519.PublicKey)

	t.Run("at the cap it verifies", func(t *testing.T) {
		msg := make([]byte, MaxEd25519MessageBytes)
		cd, err := ABI.Pack(VerifyEd25519RawMessageMethod, []byte(pub), msg, ed25519.Sign(priv, msg))
		require.NoError(t, err)

		bz, err := p.Run(nil, &vm.Contract{Input: cd}, true)
		require.NoError(t, err)

		method := ABI.Methods[VerifyEd25519RawMessageMethod]
		out, err := method.Outputs.Unpack(bz)
		require.NoError(t, err)
		require.Equal(t, []interface{}{true}, out)
	})

	t.Run("past the cap it reverts", func(t *testing.T) {
		msg := make([]byte, MaxEd25519MessageBytes+1)
		cd, err := ABI.Pack(VerifyEd25519RawMessageMethod, []byte(pub), msg, ed25519.Sign(priv, msg))
		require.NoError(t, err)

		bz, err := p.Run(nil, &vm.Contract{Input: cd}, true)

		require.Nil(t, bz, "an oversized message must not produce a verification result")
		require.Error(t, err)
		require.Contains(t, err.Error(), "message too large")
	})
}

// TestLargeMessageLoopIsGasProhibitive is the abuse shape the finding describes:
// a contract parks one large message in memory (paying its calldata once) and
// loops STATICCALLs over it. What bounds that loop is the per-call gas, so the
// number of verifications a single block can be made to run must drop sharply
// against the old flat price.
func TestLargeMessageLoopIsGasProhibitive(t *testing.T) {
	p, err := NewPrecompile()
	require.NoError(t, err)

	const (
		blockGasLimit = uint64(100_000_000)
		oldFlatGas    = uint64(4000) // the pre-fix price, at any message length
	)

	gas := p.RequiredGas(rawMessageCalldata(t, MaxEd25519MessageBytes))

	iterationsNow := blockGasLimit / gas
	iterationsBefore := blockGasLimit / oldFlatGas

	require.Less(t, iterationsNow, iterationsBefore/10,
		"a cap-sized message must buy at least 10x fewer verifications per block "+
			"than the old flat price did (now %d, before %d)", iterationsNow, iterationsBefore)
}

// BenchmarkVerifyEd25519RawMessage shows the cost curve the pricing has to
// track. The gas/us column is the one to read: under the old flat price it fell
// away as the message grew (the same 4000 gas bought steadily more CPU), which
// is the finding. With the per-word term it holds up instead.
//
//	go test ./precompiles/usigverifier/ -run '^$' -bench VerifyEd25519RawMessage -benchmem
func BenchmarkVerifyEd25519RawMessage(b *testing.B) {
	p, err := NewPrecompile()
	require.NoError(b, err)

	priv := ed25519.NewKeyFromSeed(testSeed)
	pub := priv.Public().(ed25519.PublicKey)
	method := ABI.Methods[VerifyEd25519RawMessageMethod]

	for _, msgLen := range []int{32, 1024, 8 * 1024, 64 * 1024, MaxEd25519MessageBytes} {
		msg := make([]byte, msgLen)
		sig := ed25519.Sign(priv, msg)
		args := []interface{}{[]byte(pub), msg, sig}
		gas := p.RequiredGas(rawMessageCalldata(b, msgLen))

		b.Run(fmt.Sprintf("msg=%dB", msgLen), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := p.VerifyEd25519RawMessage(&method, args); err != nil {
					b.Fatal(err)
				}
			}
			usPerOp := float64(b.Elapsed().Nanoseconds()) / float64(b.N) / 1000
			b.ReportMetric(float64(gas), "gas/op")
			b.ReportMetric(float64(gas)/usPerOp, "gas/us")
		})
	}
}

// BenchmarkRequiredGas checks the pricing itself stays cheap — it runs on every
// call, before any validation, so it must not become the expensive part.
//
//	go test ./precompiles/usigverifier/ -run '^$' -bench RequiredGas -benchmem
func BenchmarkRequiredGas(b *testing.B) {
	p, err := NewPrecompile()
	require.NoError(b, err)

	cd, err := ABI.Pack(
		VerifyEd25519RawMessageMethod,
		make([]byte, ed25519.PublicKeySize),
		make([]byte, MaxEd25519MessageBytes),
		make([]byte, ed25519.SignatureSize),
	)
	require.NoError(b, err)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if p.RequiredGas(cd) == 0 {
			b.Fatal("unexpected zero gas")
		}
	}
}
