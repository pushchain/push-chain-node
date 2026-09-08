package web2

import (
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi"
)

// web2Method mirrors the Web2QueryEnvelope method enum from the read spec.
type web2Method uint8

const (
	web2MethodGet  web2Method = 0
	web2MethodPost web2Method = 1
)

// extractValueType mirrors the Web2Extract valueType enum.
type extractValueType uint8

const (
	valueTypeUint256 extractValueType = 0
	valueTypeInt256  extractValueType = 1
	valueTypeBool    extractValueType = 2
	valueTypeString  extractValueType = 3
	valueTypeBytes   extractValueType = 4
)

// extractMode mirrors the Web2Extract mode enum.
type extractMode uint8

// modeIdentical is the only supported aggregation mode: quorum on identical
// result bytes. More modes (e.g. median) need core-side aggregation first.
const modeIdentical extractMode = 0

// web2Extract is one declared field to pull out of the JSON response.
type web2Extract struct {
	Path      string // JSONPath into the response, e.g. "$.data.price"
	ValueType extractValueType
	Mode      extractMode
	Decimals  uint8 // numeric JSON scaled by 10^decimals before encoding
}

// web2QueryEnvelope is the decoded abi.encode(Web2QueryEnvelope) query.
type web2QueryEnvelope struct {
	Method    web2Method
	URL       string
	Headers   []byte // canonical JSON object of header name -> value
	Body      []byte // POST only
	TimeoutMs uint64
	Extract   []web2Extract
}

var web2EnvelopeArgs = mustReadArgs(abi.ArgumentMarshaling{Type: "tuple", Components: []abi.ArgumentMarshaling{
	{Name: "method", Type: "uint8"},
	{Name: "url", Type: "string"},
	{Name: "headers", Type: "bytes"},
	{Name: "body", Type: "bytes"},
	{Name: "timeoutMs", Type: "uint64"},
	{Name: "extract", Type: "tuple[]", Components: []abi.ArgumentMarshaling{
		{Name: "path", Type: "string"},
		{Name: "valueType", Type: "uint8"},
		{Name: "mode", Type: "uint8"},
		{Name: "decimals", Type: "uint8"},
	}},
}})

func mustReadArgs(marshalings ...abi.ArgumentMarshaling) abi.Arguments {
	args := make(abi.Arguments, 0, len(marshalings))
	for i, m := range marshalings {
		if m.Name == "" {
			m.Name = fmt.Sprintf("arg%d", i)
		}
		typ, err := abi.NewType(m.Type, "", m.Components)
		if err != nil {
			panic(fmt.Sprintf("web2: invalid abi type %q: %v", m.Type, err))
		}
		args = append(args, abi.Argument{Name: m.Name, Type: typ})
	}
	return args
}

type rawWeb2Extract struct {
	Path      string
	ValueType uint8
	Mode      uint8
	Decimals  uint8
}

type rawWeb2Envelope struct {
	Method    uint8
	Url       string
	Headers   []byte
	Body      []byte
	TimeoutMs uint64
	Extract   []rawWeb2Extract
}

// decodeWeb2QueryEnvelope decodes ReadSpec.query for web2 destinations.
func decodeWeb2QueryEnvelope(query []byte) (*web2QueryEnvelope, error) {
	vals, err := web2EnvelopeArgs.Unpack(query)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack Web2QueryEnvelope: %w", err)
	}
	raw := *abi.ConvertType(vals[0], new(rawWeb2Envelope)).(*rawWeb2Envelope)

	env := &web2QueryEnvelope{
		Method:    web2Method(raw.Method),
		URL:       raw.Url,
		Headers:   raw.Headers,
		Body:      raw.Body,
		TimeoutMs: raw.TimeoutMs,
	}
	for _, e := range raw.Extract {
		env.Extract = append(env.Extract, web2Extract{
			Path:      e.Path,
			ValueType: extractValueType(e.ValueType),
			Mode:      extractMode(e.Mode),
			Decimals:  e.Decimals,
		})
	}

	if env.Method > web2MethodPost {
		return nil, fmt.Errorf("unknown web2 method %d", env.Method)
	}
	if len(env.Extract) == 0 {
		return nil, fmt.Errorf("envelope has no extract entries")
	}
	if len(env.Extract) > maxExtractEntries {
		return nil, fmt.Errorf("envelope has %d extract entries, max %d", len(env.Extract), maxExtractEntries)
	}
	for _, e := range env.Extract {
		if e.ValueType > valueTypeBytes {
			return nil, fmt.Errorf("unknown extract value type %d", e.ValueType)
		}
		if e.Mode != modeIdentical {
			return nil, fmt.Errorf("unsupported extract mode %d, only IDENTICAL", e.Mode)
		}
	}
	return env, nil
}
