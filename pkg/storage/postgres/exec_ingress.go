package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/jackc/pgx/v5"
)

func ingressCanonical(data []byte, value any) error {
	if len(data) == 0 || len(data) > 64<<10 || json.Unmarshal(data, value) != nil {
		return port.ErrExecutionConflict
	}
	encoded, err := json.Marshal(value)
	if err != nil || !bytes.Equal(data, encoded) {
		return port.ErrExecutionConflict
	}
	return nil
}

func readIngressOwner(ctx context.Context, q querier) (o port.ExecutionIngressOwner, err error) {
	var policy, digest, binding, bindingDigest, pin []byte
	err = q.QueryRow(ctx, `SELECT o.policy,o.policy_digest,o.binding,o.binding_digest,c.ingress_policy_digest
 FROM exec_source_owner o JOIN exec_control c ON c.id=o.id WHERE o.id`).Scan(&policy, &digest, &binding, &bindingDigest, &pin)
	if errors.Is(err, pgx.ErrNoRows) {
		e := q.QueryRow(ctx, "SELECT ingress_policy_digest FROM exec_control WHERE id").Scan(&pin)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return o, e
		}
		if pin != nil {
			return o, port.ErrExecutionConflict
		}
		return o, port.ErrNotFound
	}
	if err != nil {
		return o, err
	}
	if ingressCanonical(policy, &o.Policy) != nil {
		return o, port.ErrExecutionConflict
	}
	o.PolicyDigest, err = o.Policy.Digest()
	if err != nil || !bytes.Equal(digest, o.PolicyDigest[:]) || !bytes.Equal(pin, digest) {
		return o, port.ErrExecutionConflict
	}
	c, err := scanExecutionControl(q.QueryRow(ctx, executionControlSQL))
	if err != nil {
		return o, err
	}
	if o.Policy.Chain != c.Chain || o.Policy.Anchor != c.Anchor || o.Policy.AnchorRoot != c.AnchorRoot {
		return o, port.ErrExecutionConflict
	}
	if binding == nil && bindingDigest == nil {
		return o, nil
	}
	o.Binding = new(port.ExecutionIngressBinding)
	if ingressCanonical(binding, o.Binding) != nil || o.Binding.PolicyDigest != o.PolicyDigest {
		return o, port.ErrExecutionConflict
	}
	o.BindingDigest, err = o.Binding.Digest()
	if err != nil || !bytes.Equal(bindingDigest, o.BindingDigest[:]) {
		return o, port.ErrExecutionConflict
	}
	return o, nil
}

// ConfigureExecutionIngress finishes before API exposure even when execution
// configuration is removed. API roles inspect ownership without adopting it.
func (s *Store) ConfigureExecutionIngress(ctx context.Context, policy *port.ExecutionIngressPolicy, readOnly bool) error {
	if policy != nil {
		if err := policy.Validate(); err != nil {
			return err
		}
	}
	check := func(q querier) error {
		o, err := readIngressOwner(ctx, q)
		if errors.Is(err, port.ErrNotFound) {
			if policy == nil {
				return nil
			}
			if readOnly {
				return port.ErrReadOnly
			}
			c, err := scanActiveExecutionControl(q.QueryRow(ctx, executionControlSQL+" FOR UPDATE"))
			if err != nil {
				return err
			}
			if policy.Chain != c.Chain || policy.Anchor != c.Anchor || policy.AnchorRoot != c.AnchorRoot {
				return port.ErrExecutionConflict
			}
			if !c.BaselineReady || c.Visible != c.Anchor {
				return port.ErrExecutionCoverage
			}
			var full bool
			if err := q.QueryRow(ctx, `SELECT coverage='full' AND bootstrap_digest IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM exec_parts) FROM exec_control WHERE id`).Scan(&full); err != nil {
				return err
			}
			if !full {
				return port.ErrExecutionCoverage
			}
			encoded, _ := json.Marshal(policy)
			digest, _ := policy.Digest()
			if _, err := q.Exec(ctx, "UPDATE exec_control SET ingress_policy_digest=$1 WHERE id", digest[:]); err != nil {
				return err
			}
			_, err = q.Exec(ctx, "INSERT INTO exec_source_owner(id,policy,policy_digest) VALUES(true,$1,$2)", encoded, digest[:])
			return err
		}
		if err != nil {
			return err
		}
		if policy == nil {
			if readOnly {
				return nil
			}
			return port.ErrExecutionConflict
		}
		if o.Policy != *policy {
			return port.ErrExecutionConflict
		}
		return nil
	}
	if readOnly {
		return check(s.q(ctx))
	}
	if policy == nil {
		if err := s.write(); err != nil {
			return err
		}
		if s.executionReadBound(ctx) || s.boundTx(ctx) != nil {
			return port.ErrReadOnly
		}
		return check(s.q(ctx))
	}
	if err := s.rewindWriteAllowed(ctx); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock("+executionLockSQL+")"); err != nil {
			return err
		}
		if policy != nil {
			if err := executionDurable(ctx, tx); err != nil {
				return err
			}
		}
		return check(tx)
	})
}

func (s *Store) BindExecutionIngress(ctx context.Context, binding port.ExecutionIngressBinding) error {
	digest, err := binding.Digest()
	if err != nil {
		return err
	}
	if err := s.rewindWriteAllowed(ctx); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock("+executionLockSQL+")"); err != nil {
			return err
		}
		o, err := readIngressOwner(ctx, tx)
		if err != nil {
			return err
		}
		if o.PolicyDigest != binding.PolicyDigest {
			return port.ErrExecutionConflict
		}
		if o.Binding != nil {
			if o.BindingDigest != digest {
				return port.ErrExecutionConflict
			}
			return nil
		}
		if _, err = scanActiveExecutionControl(tx.QueryRow(ctx, executionControlSQL)); err != nil {
			return err
		}
		if err = executionDurable(ctx, tx); err != nil {
			return err
		}
		encoded, _ := json.Marshal(binding)
		_, err = tx.Exec(ctx, "UPDATE exec_source_owner SET binding=$1,binding_digest=$2 WHERE id", encoded, digest[:])
		return err
	})
}

func (s *Store) ExecutionIngressOwner(ctx context.Context) (port.ExecutionIngressOwner, error) {
	return readIngressOwner(ctx, s.q(ctx))
}

func refuseIngressReset(ctx context.Context, q querier) error {
	_, err := readIngressOwner(ctx, q)
	if errors.Is(err, port.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return port.ErrExecutionConflict
}

var _ port.ExecutionIngressOwnerStore = (*Store)(nil)
