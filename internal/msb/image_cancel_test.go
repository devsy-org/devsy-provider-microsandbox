package msb

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"io"
	"net"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/partial"
)

func (s *imageUserSuite) TestCancellationClosesBlockedLayer() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, writer := net.Pipe()
	defer func() { _ = reader.Close(); _ = writer.Close() }()
	started := make(chan struct{}, 1)
	img := accountLayerImage{Image: empty.Image, layer: blockingAccountLayer{
		reader: &startedAccountReader{Conn: reader, started: started},
	}}
	done := make(chan error, 1)
	go func() { _, err := ownerFromImage(ctx, img, "vscode"); done <- err }()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		s.FailNow("account layer read did not start")
	}
	cancel()
	select {
	case err := <-done:
		s.ErrorIs(err, context.Canceled)
	case <-time.After(10 * time.Second):
		s.FailNow("cancelled account layer remained blocked")
	}
}

func (s *imageUserSuite) TestCancellationWithCompressedLayer() {
	data := make([]byte, 1<<20)
	_, err := rand.Read(data)
	s.Require().NoError(err)
	var buf bytes.Buffer
	zipped := gzip.NewWriter(&buf)
	writer := tar.NewWriter(zipped)
	s.Require().
		NoError(writer.WriteHeader(&tar.Header{Name: "unrelated", Size: int64(len(data)), Mode: 0o644}))
	_, err = writer.Write(data)
	s.Require().NoError(err)
	s.Require().NoError(writer.Close())
	s.Require().NoError(zipped.Close())
	for range 10 {
		ctx, cancel := context.WithCancel(context.Background())
		input := &cancelingAccountReader{Reader: bytes.NewReader(buf.Bytes()), cancel: cancel}
		source := io.NopCloser(input)
		layer, err := partial.CompressedToLayer(compressedAccountLayer{reader: source})
		s.Require().NoError(err)
		_, err = ownerFromImage(ctx, accountLayerImage{Image: empty.Image, layer: layer}, "vscode")
		cancel()
		s.ErrorIs(err, context.Canceled)
		s.Positive(input.Len(), "cancelled extraction must not drain remaining compressed data")
	}
}

type accountLayerImage struct {
	v1.Image
	layer v1.Layer
}

func (img accountLayerImage) Layers() ([]v1.Layer, error) { return []v1.Layer{img.layer}, nil }

type blockingAccountLayer struct {
	v1.Layer
	reader io.ReadCloser
}

func (layer blockingAccountLayer) Compressed() (io.ReadCloser, error) { return layer.reader, nil }

type startedAccountReader struct {
	net.Conn
	started chan<- struct{}
}

func (reader *startedAccountReader) Read(buf []byte) (int, error) {
	select {
	case reader.started <- struct{}{}:
	default:
	}
	return reader.Conn.Read(buf)
}

type compressedAccountLayer struct {
	v1.Layer
	reader io.ReadCloser
}

func (layer compressedAccountLayer) Compressed() (io.ReadCloser, error) { return layer.reader, nil }

type cancelingAccountReader struct {
	*bytes.Reader
	cancel context.CancelFunc
}

func (reader *cancelingAccountReader) Read(buf []byte) (int, error) {
	n, err := reader.Reader.Read(buf)
	// Cancel before the final compressed block exercises decoder cleanup during Read.
	if reader.Len() < 64<<10 {
		reader.cancel()
	}
	return n, err
}
