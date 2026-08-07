// Package web2 executes web2 (HTTP) read requests: it fetches the declared
// endpoint, extracts the declared JSON fields, and canonically encodes them so
// read results are byte-identical across validators.
package web2

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/externalchains/common"
	ucallbacktypes "github.com/pushchain/push-chain-node/x/ucallback/types"
)

const (
	// DestinationPrefix identifies web2 read destinations, e.g. "web2:https".
	DestinationPrefix = "web2:"

	maxResponseBytes  = 64 * 1024
	maxExtractEntries = 16
	defaultTimeout    = 5 * time.Second
	maxTimeout        = 15 * time.Second
	maxRedirects      = 5
)

// errBlockedRequest marks a request rejected by the SSRF guard (private/internal
// address, disallowed redirect). It is deterministic: every honest validator
// rejects the same envelope identically, so it becomes a votable ERROR rather
// than a transient retry.
var errBlockedRequest = errors.New("request blocked by ssrf guard")

// TODO(core): a web2 read makes every validator fetch an attacker-chosen URL,
// so the fee is the only thing pricing that outbound work. The fee must NEVER
// be fully refunded on failure or no-quorum: a full refund lets an attacker
// drive the whole validator set at any endpoint for only tx gas (griefing /
// reflected load). Charge for execution regardless of read outcome.

// Executor implements common.ReadRequestHandler for web2 destinations.
type Executor struct {
	httpClient *http.Client
	logger     zerolog.Logger
	// allowInsecureURL disables the https-only rule (tests only)
	allowInsecureURL bool
}

// NewExecutor creates a web2 read executor. Its HTTP client dials through an
// SSRF guard that blocks private/internal addresses on the initial request and
// on every redirect hop, and only connects to the exact IP it vetted (so DNS
// rebinding cannot swap in an internal address between check and dial).
func NewExecutor(logger zerolog.Logger) *Executor {
	e := &Executor{
		logger: logger.With().Str("component", "web2_read_executor").Logger(),
	}
	e.httpClient = &http.Client{
		Timeout:       maxTimeout,
		Transport:     &http.Transport{DialContext: e.dialContext},
		CheckRedirect: e.checkRedirect,
	}
	return e
}

// dialContext resolves the target host and refuses any non-public address, then
// dials the vetted IP directly. Tests set allowInsecureURL to reach httptest
// servers on loopback.
func (e *Executor) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if e.allowInsecureURL {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no addresses for %s", host)
	}
	for _, ip := range ips {
		if isDisallowedIP(ip.IP) {
			return nil, fmt.Errorf("%w: %s resolves to non-public address %s", errBlockedRequest, host, ip.IP)
		}
	}

	dialer := &net.Dialer{}
	var lastErr error
	for _, ip := range ips {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if err != nil {
			lastErr = err
			continue
		}
		return conn, nil
	}
	return nil, lastErr
}

// checkRedirect keeps redirects https-only and bounded. The dialer still vets
// every hop's address; this only rejects scheme downgrades and redirect loops.
func (e *Executor) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("%w: too many redirects", errBlockedRequest)
	}
	if !e.allowInsecureURL && req.URL.Scheme != "https" {
		return fmt.Errorf("%w: redirect to non-https url", errBlockedRequest)
	}
	return nil
}

// isDisallowedIP reports whether an IP is one the guard must never connect to:
// loopback, private (RFC1918 / ULA), link-local (incl. 169.254.169.254 cloud
// metadata), carrier-grade NAT, multicast, or the unspecified address.
func isDisallowedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return true
	}
	// Carrier-grade NAT 100.64.0.0/10 (RFC 6598) is not covered by IsPrivate.
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1]&0xc0 == 64 {
		return true
	}
	return false
}

// ExecuteRead fetches the endpoint declared in the envelope, extracts the
// declared fields, and abi-encodes them in extract order. Deterministic
// failures (bad envelope, non-JSON response, missing path, 4xx) are votable
// ERROR observations; transport failures and 5xx are transient errors.
func (e *Executor) ExecuteRead(ctx context.Context, req *ucallbacktypes.ReadRequest) (*ucallbacktypes.ReadResult, error) {
	env, err := decodeWeb2QueryEnvelope(req.Query)
	if err != nil {
		return common.NewReadErrorResult(err), nil
	}

	if err := e.validateEnvelope(env); err != nil {
		return common.NewReadErrorResult(err), nil
	}

	body, errResult, err := e.fetch(ctx, env)
	if err != nil {
		return nil, err // transient
	}
	if errResult != nil {
		return errResult, nil
	}

	resultData, err := extractAndEncode(body, env.Extract)
	if err != nil {
		return common.NewReadErrorResult(err), nil
	}

	// web2 has no block height or hash; the ballot covers result data only
	return &ucallbacktypes.ReadResult{
		Status:     ucallbacktypes.ReadStatus_READ_STATUS_SUCCESS,
		ResultData: resultData,
	}, nil
}

// validateEnvelope enforces the v1 request constraints.
func (e *Executor) validateEnvelope(env *web2QueryEnvelope) error {
	if !e.allowInsecureURL && !strings.HasPrefix(env.URL, "https://") {
		return fmt.Errorf("url must be https")
	}
	if env.Method == web2MethodGet && len(env.Body) > 0 {
		return fmt.Errorf("GET request must not have a body")
	}
	return nil
}

// fetch performs the HTTP request. Returns (body, nil, nil) on success,
// (nil, errorResult, nil) on deterministic failure, (nil, nil, err) on
// transient failure.
func (e *Executor) fetch(ctx context.Context, env *web2QueryEnvelope) ([]byte, *ucallbacktypes.ReadResult, error) {
	timeout := defaultTimeout
	if env.TimeoutMs > 0 {
		timeout = min(time.Duration(env.TimeoutMs)*time.Millisecond, maxTimeout)
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	method := http.MethodGet
	var reqBody io.Reader
	if env.Method == web2MethodPost {
		method = http.MethodPost
		reqBody = bytes.NewReader(env.Body)
	}

	httpReq, err := http.NewRequestWithContext(reqCtx, method, env.URL, reqBody)
	if err != nil {
		return nil, common.NewReadErrorResult(fmt.Errorf("invalid request: %w", err)), nil
	}

	if len(env.Headers) > 0 {
		var headers map[string]string
		if err := json.Unmarshal(env.Headers, &headers); err != nil {
			return nil, common.NewReadErrorResult(fmt.Errorf("invalid headers encoding: %w", err)), nil
		}
		for name, value := range headers {
			httpReq.Header.Set(name, value)
		}
	}

	resp, err := e.httpClient.Do(httpReq)
	if err != nil {
		// A guard rejection is the same for every validator: votable ERROR.
		// Any other transport error may be transient.
		if errors.Is(err, errBlockedRequest) {
			return nil, common.NewReadErrorResult(fmt.Errorf("request blocked: %w", err)), nil
		}
		return nil, nil, fmt.Errorf("request failed: %w", err) // transient
	}
	defer func() { _ = resp.Body.Close() }()

	// 4xx is a deterministic answer from the endpoint; 5xx is the endpoint
	// having a bad moment
	if resp.StatusCode >= 500 {
		return nil, nil, fmt.Errorf("endpoint returned status %d", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, common.NewReadErrorResult(fmt.Errorf("endpoint returned status %d", resp.StatusCode)), nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response: %w", err) // transient
	}
	if len(body) > maxResponseBytes {
		return nil, common.NewReadErrorResult(fmt.Errorf("response exceeds %d bytes", maxResponseBytes)), nil
	}

	return body, nil, nil
}

// extractAndEncode applies each extract spec to the JSON response and
// abi-encodes the values in extract order.
func extractAndEncode(body []byte, extracts []web2Extract) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var root any
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("response is not valid JSON: %w", err)
	}

	args := make(abi.Arguments, 0, len(extracts))
	values := make([]any, 0, len(extracts))
	for _, ex := range extracts {
		raw, err := resolveJSONPath(root, ex.Path)
		if err != nil {
			return nil, err
		}

		value, abiType, err := convertValue(raw, ex)
		if err != nil {
			return nil, fmt.Errorf("path %s: %w", ex.Path, err)
		}
		args = append(args, abi.Argument{Name: "v", Type: abiType})
		values = append(values, value)
	}

	encoded, err := args.Pack(values...)
	if err != nil {
		return nil, fmt.Errorf("failed to encode result: %w", err)
	}
	return encoded, nil
}

var (
	abiUint256, _ = abi.NewType("uint256", "", nil)
	abiInt256, _  = abi.NewType("int256", "", nil)
	abiBool, _    = abi.NewType("bool", "", nil)
	abiString, _  = abi.NewType("string", "", nil)
	abiBytes, _   = abi.NewType("bytes", "", nil)
)

// convertValue converts a JSON value to the declared abi value.
func convertValue(raw any, ex web2Extract) (any, abi.Type, error) {
	switch ex.ValueType {
	case valueTypeUint256, valueTypeInt256:
		num, err := scaledInteger(raw, ex.Decimals)
		if err != nil {
			return nil, abi.Type{}, err
		}
		if ex.ValueType == valueTypeUint256 {
			if num.Sign() < 0 || num.BitLen() > 256 {
				return nil, abi.Type{}, fmt.Errorf("value out of uint256 range")
			}
			return num, abiUint256, nil
		}
		if num.BitLen() > 255 {
			return nil, abi.Type{}, fmt.Errorf("value out of int256 range")
		}
		return num, abiInt256, nil

	case valueTypeBool:
		b, ok := raw.(bool)
		if !ok {
			return nil, abi.Type{}, fmt.Errorf("expected bool, got %T", raw)
		}
		return b, abiBool, nil

	case valueTypeString:
		s, ok := raw.(string)
		if !ok {
			return nil, abi.Type{}, fmt.Errorf("expected string, got %T", raw)
		}
		return s, abiString, nil

	case valueTypeBytes:
		s, ok := raw.(string)
		if !ok || !strings.HasPrefix(s, "0x") {
			return nil, abi.Type{}, fmt.Errorf("expected 0x-prefixed hex string")
		}
		decoded, err := hex.DecodeString(strings.TrimPrefix(s, "0x"))
		if err != nil {
			return nil, abi.Type{}, fmt.Errorf("invalid hex: %w", err)
		}
		return decoded, abiBytes, nil

	default:
		return nil, abi.Type{}, fmt.Errorf("unknown value type %d", ex.ValueType)
	}
}

// scaledInteger parses a JSON number (or numeric string), scales it by
// 10^decimals, and truncates to an integer. big.Rat keeps float-formatted
// JSON exact (e.g. "3512.4471" with 8 decimals -> 351244710000).
func scaledInteger(raw any, decimals uint8) (*big.Int, error) {
	var numStr string
	switch v := raw.(type) {
	case json.Number:
		numStr = v.String()
	case string:
		numStr = v
	default:
		return nil, fmt.Errorf("expected number, got %T", raw)
	}

	rat, ok := new(big.Rat).SetString(numStr)
	if !ok {
		return nil, fmt.Errorf("invalid number %q", numStr)
	}

	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	rat.Mul(rat, new(big.Rat).SetInt(scale))

	return new(big.Int).Quo(rat.Num(), rat.Denom()), nil
}

// resolveJSONPath resolves a minimal JSONPath subset: "$" root, dot fields and
// array indexes, e.g. "$.data.items[0].price".
func resolveJSONPath(root any, path string) (any, error) {
	if !strings.HasPrefix(path, "$") {
		return nil, fmt.Errorf("path %s must start with $", path)
	}

	current := root
	rest := strings.TrimPrefix(path, "$")
	for _, segment := range strings.Split(rest, ".") {
		if segment == "" {
			continue
		}

		field := segment
		var indexes []int
		for strings.HasSuffix(field, "]") {
			open := strings.LastIndex(field, "[")
			if open < 0 {
				return nil, fmt.Errorf("path %s has malformed index in %q", path, segment)
			}
			idx, err := strconv.Atoi(field[open+1 : len(field)-1])
			if err != nil || idx < 0 {
				return nil, fmt.Errorf("path %s has invalid index in %q", path, segment)
			}
			indexes = append([]int{idx}, indexes...)
			field = field[:open]
		}

		if field != "" {
			obj, ok := current.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("path %s: %q is not an object", path, field)
			}
			value, ok := obj[field]
			if !ok {
				return nil, fmt.Errorf("path %s: field %q not found", path, field)
			}
			current = value
		}

		for _, idx := range indexes {
			arr, ok := current.([]any)
			if !ok {
				return nil, fmt.Errorf("path %s: indexing into non-array", path)
			}
			if idx >= len(arr) {
				return nil, fmt.Errorf("path %s: index %d out of range", path, idx)
			}
			current = arr[idx]
		}
	}

	return current, nil
}
