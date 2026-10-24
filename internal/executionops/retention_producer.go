package executionops

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/0xmhha/indexer-go/execwire/retention"
)

type retentionOutput struct {
	bytes.Buffer
	overflow bool
	limit    int
}

func (b *retentionOutput) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) > b.limit-b.Len() {
		b.overflow = true
		p = p[:max(0, b.limit-b.Len())]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}
func runRetentionProducer(ctx context.Context, o RetentionOptions, p retention.Permit) (retention.Receipt, error) {
	if !filepath.IsAbs(o.Producer) || !filepath.IsAbs(o.ProducerConfig) {
		return retention.Receipt{}, fmt.Errorf("supervised collect needs absolute producer and producer-config paths")
	}
	read, write, e := os.Pipe()
	if e != nil {
		return retention.Receipt{}, e
	}
	defer func() { _ = read.Close(); _ = write.Close() }()
	hello := retention.Supervision{Version: 1}
	hello.PermitDigest, e = retention.Digest(p)
	if e != nil {
		return retention.Receipt{}, e
	}
	if _, e = rand.Read(hello.Challenge[:]); e != nil {
		return retention.Receipt{}, e
	}
	cmd := exec.CommandContext(ctx, o.Producer, "-action", "apply", "-config", o.ProducerConfig, "-permit", o.Permit)
	cmd.ExtraFiles = []*os.File{read}
	// Producer needs no PostgreSQL/RPC credentials or live-node environment.
	cmd.Env = []string{"PATH=/usr/bin:/bin", "TZ=UTC"}
	if temp := os.Getenv("TMPDIR"); temp != "" {
		cmd.Env = append(cmd.Env, "TMPDIR="+temp)
	}
	stdout := &retentionOutput{limit: retention.MaxArtifact}
	stderr := &retentionOutput{limit: 4096}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if e = cmd.Start(); e != nil {
		return retention.Receipt{}, e
	}
	_ = read.Close()
	if e = json.NewEncoder(write).Encode(hello); e != nil {
		_ = write.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return retention.Receipt{}, e
	}
	e = cmd.Wait()
	_ = write.Close()
	if e != nil {
		return retention.Receipt{}, fmt.Errorf("supervised producer failed: %w (%s)", e, strings.TrimSpace(stderr.String()))
	}
	if stdout.overflow {
		return retention.Receipt{}, retention.ErrUnsafe
	}
	var reply retention.SupervisedReply
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&reply); e != nil {
		return retention.Receipt{}, e
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || reply.Version != 1 || reply.Challenge != hello.Challenge || reply.Receipt.Match(p) != nil {
		return retention.Receipt{}, retention.ErrUnsafe
	}
	return reply.Receipt, nil
}
