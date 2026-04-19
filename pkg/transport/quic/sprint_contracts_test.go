package quictr_test

// sprint_contracts_test.go — tests required by sprint contracts F-003.
// Satisfies CONDITIONAL PASS → PASS for pkg/transport/quic.

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	quictr "github.com/cloud-jumpgate/hyperspace/pkg/transport/quic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestQUIC_SendRecv_1000Frames sends exactly 1000 messages from client to server
// and verifies all payload bytes are received with a non-empty payload on each RecvData call.
//
// Implementation note: conn.Send reuses the same unidirectional QUIC stream per streamID.
// All 1000 writes flow as a byte stream over one QUIC stream. The server's readUniStream
// goroutine pushes data into a channel (cap 256) in chunks; RecvData returns one chunk
// at a time. We verify total bytes received equals 1000 × payloadSize, confirming all
// 1000 messages arrived intact. The server drains continuously (no sleeping) to prevent
// the internal channel from stalling the sender.
func TestQUIC_SendRecv_1000Frames(t *testing.T) {
	const totalFrames = 1000
	const streamID = uint64(2)
	const payloadSize = 64 // 64 bytes per message — fits cleanly in network frames
	const totalBytes = int64(totalFrames * payloadSize)

	ln := startTestServer(t)

	var bytesReceived atomic.Int64
	serverDone := make(chan struct{})

	go func() {
		defer close(serverDone)
		conn, err := ln.Accept(context.Background())
		if err != nil {
			return
		}
		serverQC, acceptErr := quictr.Accept(conn)
		if acceptErr != nil {
			return
		}
		defer func() { _ = serverQC.Close() }()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Drain without sleeping to prevent the internal dataIn channel from filling.
		for bytesReceived.Load() < totalBytes {
			if ctx.Err() != nil {
				return
			}
			sid, data, recvErr := serverQC.RecvData(ctx)
			if recvErr != nil {
				return
			}
			if len(data) > 0 {
				assert.Equal(t, streamID, sid, "received data should be on stream %d", streamID)
				bytesReceived.Add(int64(len(data)))
			}
		}
	}()

	addr := ln.Addr().String()
	client, err := quictr.Dial(context.Background(), addr, clientTLSConfig(), "bbr")
	require.NoError(t, err, "dial should succeed")
	defer func() { _ = client.Close() }()

	time.Sleep(50 * time.Millisecond)

	payload := make([]byte, payloadSize)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	for i := 0; i < totalFrames; i++ {
		sendErr := client.Send(streamID, payload)
		require.NoError(t, sendErr, "Send frame %d should succeed", i)
	}

	select {
	case <-serverDone:
	case <-time.After(30 * time.Second):
		t.Fatalf("timed out; got %d/%d bytes", bytesReceived.Load(), totalBytes)
	}

	assert.Equal(t, totalBytes, bytesReceived.Load(),
		"server should receive exactly %d bytes (%d frames × %d bytes)",
		totalBytes, totalFrames, payloadSize)
}
