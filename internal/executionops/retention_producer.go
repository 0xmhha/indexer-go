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

	"github.com/0xmhha/indexer-go/execwire"
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
func runValidatedRetentionProducer(ctx context.Context, o RetentionOptions, p retention.Permit, validate func(context.Context, retention.Permit, retention.IndexerBackup) error) (retention.Receipt, error) {
	var digest execwire.Hash
	if o.Action == "restore-admit" && o.BackupReceipt == "" {
		return retention.Receipt{}, retention.ErrUnsafe
	}
	if o.BackupReceipt != "" {
		if validate == nil {
			return retention.Receipt{}, retention.ErrUnsafe
		}
		var external retention.IndexerBackup
		if err := retention.ReadArtifact(o.BackupReceipt, &external); err != nil {
			return retention.Receipt{}, err
		}
		if err := validate(ctx, p, external); err != nil {
			return retention.Receipt{}, err
		}
		var err error
		digest, err = retention.Digest(external)
		if err != nil {
			return retention.Receipt{}, err
		}
	}
	return runRetentionProducer(ctx, o, p, digest)
}

func runRetentionProducer(ctx context.Context, o RetentionOptions, p retention.Permit, validatedDigest execwire.Hash) (retention.Receipt, error) {
	if !filepath.IsAbs(o.Producer) || !filepath.IsAbs(o.ProducerConfig) {
		return retention.Receipt{}, fmt.Errorf("supervised collect needs absolute producer and producer-config paths")
	}
	read, write, e := os.Pipe()
	if e != nil {
		return retention.Receipt{}, e
	}
	defer func() { _ = read.Close(); _ = write.Close() }()
	hello := retention.Supervision{Version: 1}
	// Authority is the immutable value accepted by the live DB validator.
	// Never derive it again from the mutable receipt pathname.
	if (o.BackupReceipt != "" || o.Action == "restore-admit") && validatedDigest == (execwire.Hash{}) {
		return retention.Receipt{}, retention.ErrUnsafe
	}
	hello.BackupDigest = validatedDigest
	hello.PermitDigest, e = retention.Digest(p)
	if e != nil {
		return retention.Receipt{}, e
	}
	if _, e = rand.Read(hello.Challenge[:]); e != nil {
		return retention.Receipt{}, e
	}
	action := "apply"
	if o.Action == "restore-admit" {
		action = "restore-admit"
	}
	cmd := exec.CommandContext(ctx, o.Producer, "-action", action, "-config", o.ProducerConfig, "-permit", o.Permit)
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
