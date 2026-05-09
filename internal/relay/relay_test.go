package relay

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBidirectionalRelay(t *testing.T) {
	aClient, aServer := net.Pipe()
	bClient, bServer := net.Pipe()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		result, err := Bidirectional(ctx, aServer, bServer)
		if err != nil {
			t.Logf("relay ended: %v", err)
		}
		t.Logf("sent=%d received=%d", result.Stats.GetSent(), result.Stats.GetReceived())
	}()

	_, err := aClient.Write([]byte("from-a"))
	require.NoError(t, err)
	_ = aClient.SetDeadline(time.Now().Add(2 * time.Second))

	buf := make([]byte, 100)
	n, _ := bClient.Read(buf)
	assert.Equal(t, "from-a", string(buf[:n]))

	_, err = bClient.Write([]byte("from-b"))
	require.NoError(t, err)

	n, _ = aClient.Read(buf)
	assert.Equal(t, "from-b", string(buf[:n]))

	_ = aClient.Close()
	_ = bClient.Close()
	_ = aServer.Close()
	_ = bServer.Close()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("relay did not finish")
	}
}

func TestBidirectional_Cancellation(t *testing.T) {
	aClient, aServer := net.Pipe()
	bClient, bServer := net.Pipe()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _ = Bidirectional(ctx, aServer, bServer)
	_ = aClient.Close()
	_ = bClient.Close()
}
