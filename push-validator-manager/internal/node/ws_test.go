package node

import (
    "bufio"
    "context"
    "crypto/sha1"
    "encoding/base64"
    "fmt"
    "io"
    "net"
    "strings"
    "testing"
    "time"
)

// minimal server-side frame writer (no masking)
func wsWriteFrame(w *bufio.Writer, opcode byte, payload []byte) error {
    header := []byte{0x80 | (opcode & 0x0F)} // FIN + opcode
    n := len(payload)
    switch {
    case n <= 125:
        header = append(header, byte(n))
    case n <= 65535:
        header = append(header, 126, byte(n>>8), byte(n))
    default:
        header = append(header, 127, 0, 0, 0, 0, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
    }
    if _, err := w.Write(header); err != nil { return err }
    if _, err := w.Write(payload); err != nil { return err }
    return w.Flush()
}

// server-side reader for client frames (masked), returns opcode and unmasked payload
func wsReadFrame(r *bufio.Reader) (byte, []byte, error) {
    b1, err := r.ReadByte()
    if err != nil { return 0, nil, err }
    b2, err := r.ReadByte()
    if err != nil { return 0, nil, err }
    opcode := b1 & 0x0F
    masked := (b2 & 0x80) != 0
    ln := int(b2 & 0x7F)
    if ln == 126 {
        b, err := readNTest(r, 2)
        if err != nil { return 0, nil, err }
        ln = int(b[0])<<8 | int(b[1])
    } else if ln == 127 {
        b, err := readNTest(r, 8)
        if err != nil { return 0, nil, err }
        ln = int(b[4])<<24 | int(b[5])<<16 | int(b[6])<<8 | int(b[7])
    }
    var maskKey [4]byte
    if masked {
        mk, err := readNTest(r, 4)
        if err != nil { return 0, nil, err }
        copy(maskKey[:], mk)
    }
    payload, err := readNTest(r, ln)
    if err != nil { return 0, nil, err }
    if masked {
        for i := 0; i < ln; i++ { payload[i] ^= maskKey[i%4] }
    }
    return opcode, payload, nil
}

func readNTest(r *bufio.Reader, n int) ([]byte, error) {
    buf := make([]byte, n)
    _, err := io.ReadFull(r, buf)
    return buf, err
}

func TestDialAndSubscribeHeaders_JSONRPCSubprotocol(t *testing.T) {
    // Some sandboxes restrict binding; detect and skip.
    probe, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil { t.Skipf("skipping: cannot bind due to sandbox: %v", err) }
    probe.Close()

    ln, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil { t.Skipf("skipping: cannot listen: %v", err) }
    defer ln.Close()

    srvErr := make(chan error, 1)
    go func() {
        c, err := ln.Accept()
        if err != nil { srvErr <- err; return }
        defer c.Close()
        br := bufio.NewReadWriter(bufio.NewReader(c), bufio.NewWriter(c))
        // Read HTTP request
        statusLine, err := br.Reader.ReadString('\n')
        if err != nil { srvErr <- err; return }
        if !strings.HasPrefix(statusLine, "GET ") { srvErr <- fmt.Errorf("bad request line: %s", statusLine); return }
        headers := map[string]string{}
        for {
            line, err := br.Reader.ReadString('\n')
            if err != nil { srvErr <- err; return }
            line = strings.TrimRight(line, "\r\n")
            if line == "" { break }
            if i := strings.Index(line, ":"); i > 0 {
                k := strings.ToLower(strings.TrimSpace(line[:i]))
                v := strings.TrimSpace(line[i+1:])
                headers[k] = v
            }
        }
        // Validate JSONRPC subprotocol requested
        if sp := headers["sec-websocket-protocol"]; !strings.Contains(strings.ToLower(sp), "jsonrpc") {
            srvErr <- fmt.Errorf("missing jsonrpc subprotocol: %q", sp); return
        }
        key := headers["sec-websocket-key"]
        if key == "" { srvErr <- fmt.Errorf("missing sec-websocket-key") ; return }
        acc := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
        accept := base64.StdEncoding.EncodeToString(acc[:])
        // Respond 101 with subprotocol
        resp := fmt.Sprintf("HTTP/1.1 101 Switching Protocols\r\n"+
            "Upgrade: websocket\r\n"+
            "Connection: Upgrade\r\n"+
            "Sec-WebSocket-Accept: %s\r\n"+
            "Sec-WebSocket-Protocol: jsonrpc\r\n\r\n", accept)
        if _, err := br.Writer.WriteString(resp); err != nil { srvErr <- err; return }
        if err := br.Writer.Flush(); err != nil { srvErr <- err; return }
        // Read client's subscribe frame
        if _, _, err := wsReadFrame(br.Reader); err != nil { srvErr <- fmt.Errorf("read subscribe: %w", err); return }
        // Write a header event frame
        payload := []byte(`{"jsonrpc":"2.0","result":{"data":{"value":{"header":{"height":"42","time":"2024-01-01T00:00:00Z"}}}}}`)
        if err := wsWriteFrame(br.Writer, 0x1, payload); err != nil { srvErr <- err; return }
        // Give client time to read
        time.Sleep(100 * time.Millisecond)
        // Close
        _ = wsWriteFrame(br.Writer, 0x8, []byte{})
        srvErr <- nil
    }()

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    wsURL := fmt.Sprintf("ws://%s/websocket", ln.Addr().String())
    ch, err := DialAndSubscribeHeaders(ctx, wsURL)
    if err != nil { t.Fatalf("dial/subscribe error: %v", err) }
    select {
    case h, ok := <-ch:
        if !ok { t.Fatal("channel closed") }
        if h.Height != 42 { t.Fatalf("got height %d want 42", h.Height) }
    case <-time.After(2 * time.Second):
        t.Fatal("timeout waiting for header")
    }
    if e := <-srvErr; e != nil { t.Fatalf("server error: %v", e) }
}
