package executionbackup

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
)

type report struct {
	Version              int    `json:"version"`
	Status               string `json:"status"`
	Code                 string `json:"code"`
	Scope                string `json:"scope"`
	OperationalReadiness string `json:"operational_readiness"`
	RestoreVerification  string `json:"restore_verification"`
	LiveFreshness        string `json:"live_freshness"`
	ReceiptExported      bool   `json:"receipt_exported"`
}

// Run emits bounded, redacted diagnostics. No environment, DSN, signing key,
// provider subprocess or runtime configuration is consumed.
func Run(ctx context.Context, args []string, out io.Writer) int {
	r := report{Version: 1, Status: "PASS", Code: "authenticated_artifact", Scope: "offline_signed_evidence_only", OperationalReadiness: "not_authorized", RestoreVerification: "not_performed", LiveFreshness: "not_checked"}
	f := flag.NewFlagSet("execution-backup", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	var in Inputs
	f.StringVar(&in.Trust, "trust", "", "Explicit private provider trust JSON")
	f.StringVar(&in.Permit, "permit", "", "Exact offline E3 permit JSON")
	f.StringVar(&in.Attestation, "attestation", "", "External signed attestation JSON")
	f.StringVar(&in.Artifact, "artifact", "", "External packed PostgreSQL snapshot/WAL file")
	f.StringVar(&in.Output, "output", "", "Optional new private legacy receipt path (never overwritten)")
	err := f.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		_, e := io.WriteString(out, "execution-backup -trust FILE -permit FILE -attestation FILE -artifact FILE [-output NEW_FILE]\nVerify signed evidence only; no database, restore, startup or GC authorization.\n")
		if e != nil {
			return 1
		}
		return 0
	}
	if err != nil || f.NArg() != 0 {
		r.Status, r.Code = "FAIL", "invalid_arguments"
	} else {
		for _, p := range []string{in.Trust, in.Permit, in.Attestation, in.Artifact} {
			if p == "" || p == "HOLD" {
				r.Status, r.Code = "HOLD", "operator_input_missing"
			}
		}
		if r.Status == "PASS" {
			receipt, e := Verify(ctx, in)
			if e != nil {
				r.Status, r.Code = "FAIL", "evidence_rejected"
			} else if in.Output != "" {
				if e = publishReceipt(ctx, in.Output, receipt); e != nil {
					r.Status, r.Code = "FAIL", "receipt_export_failed"
				} else {
					r.ReceiptExported = true
				}
			}
		}
	}
	if ctx.Err() != nil {
		r.Status, r.Code = "FAIL", "cancelled"
	}
	if json.NewEncoder(out).Encode(r) != nil {
		return 1
	}
	if r.Status == "HOLD" {
		return 2
	}
	if r.Status == "FAIL" {
		return 1
	}
	return 0
}
