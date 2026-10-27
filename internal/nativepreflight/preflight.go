// Package nativepreflight validates declared files. It has no runtime lifecycle.
package nativepreflight

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"io"
	"math"
	"path/filepath"
	"reflect"
	"regexp"

	"strings"

	"github.com/0xmhha/indexer-go/execwire"
	format "github.com/0xmhha/indexer-go/execwire/checkpoint"
	"github.com/0xmhha/indexer-go/execwire/configfile"

	appruntime "github.com/0xmhha/indexer-go/pkg/app"
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
	"github.com/0xmhha/indexer-go/pkg/chains"
)

const (
	Pass = "PASS"
	Hold = "HOLD"
	Fail = "FAIL"
)

type Finding struct {
	Field  string `json:"field"`
	Status string `json:"status"`
	Code   string `json:"code"`
}
type Report struct {
	Version                 int       `json:"version"`
	Status                  string    `json:"status"`
	Scope                   string    `json:"scope"`
	OperationalReadiness    string    `json:"operational_readiness"`
	LiveFreshness           string    `json:"live_freshness"`
	FullStateReconstruction string    `json:"full_state_reconstruction"`
	GenesisContents         string    `json:"genesis_contents"`
	HostAndPeerVerification string    `json:"host_and_peer_verification"`
	Findings                []Finding `json:"findings"`
}

func (r Report) ExitCode() int {
	if r.Status == Pass {
		return 0
	}
	if r.Status == Hold {
		return 2
	}
	return 1
}
func (r *Report) Add(field, status, code string) {
	if len(r.Findings) >= 128 {
		r.Findings[127] = Finding{"report", Fail, "finding_limit_exceeded"}
		r.Status = Fail
		return
	}
	r.Findings = append(r.Findings, Finding{field, status, code})
	if status == Fail || status == Hold && r.Status == Pass {
		r.Status = status
	}
}

type Inputs struct{ App, Native, Checkpoint, Retention, Declaration string }

// Native contains normalized observations from a product's production schema.
// A product decoder must perform its pure production validation first.
type DeclaredPath struct{ Field, Value string }
type Native struct {
	ExtraPaths                                []DeclaredPath
	Unknown                                   Unknown
	NetworkID                                 uint64
	Peers                                     []string
	Chain                                     execwire.Chain
	Anchor                                    execwire.Ref
	Root                                      execwire.Hash
	Data, Spool, Scratch, Socket, GenesisFile string
	SpoolBytes, Reserve                       uint64
	SpoolRecords                              int
	StateProofs                               *stateproof.Selection
}
type Product struct {
	SeparateRetention, PeerMembership, CollectorScratch bool
	ConsensusProfile                                    string
	ConsensusProtocol                                   string
	HeaderCodec                                         chains.HeaderCodec
	Profile                                             string
	Decode                                              func([]byte, []byte, *Report) (Native, error)
	Consensus                                           func(execwire.Chain, chains.ConsensusPolicy) (chains.ConsensusVerifier, error)
	Identity                                            func(execwire.Chain, uint64, Unknown, *Report)
}

// Declaration is supplemental offline operator metadata, never a host manifest
// or a runtime override. Zero numeric values are known values, not placeholders.
type Declaration struct {
	StartMode           string         `json:"start_mode"`
	Profile             string         `json:"profile"`
	Chain               execwire.Chain `json:"chain"`
	NetworkID           uint64         `json:"network_id"`
	Anchor              execwire.Ref   `json:"anchor"`
	Root                execwire.Hash  `json:"root"`
	StartHeight         uint64         `json:"start_height"`
	RetainedFloor       uint64         `json:"retained_floor"`
	CheckpointPath      string         `json:"checkpoint_path"`
	CheckpointAuthority string         `json:"checkpoint_authority"`
	APFSIdentity        string         `json:"apfs_identity"`
	Enode               string         `json:"enode"`
	PGData              string         `json:"pg_data"`
	NativeBytes         uint64         `json:"native_bytes"`
	FreeBytes           uint64         `json:"free_bytes"`
	IndexerBytes        uint64         `json:"indexer_bytes"`
	CheckpointBytes     uint64         `json:"checkpoint_bytes"`
	ScratchBytes        uint64         `json:"scratch_bytes"`
	BackupBytes         uint64         `json:"backup_bytes"`
	PartTransactions    int            `json:"part_transactions"`
	APIReservation      int            `json:"api_reservation"`
	LogicalWriters      int            `json:"logical_writers"`
	FreshPolicy         string         `json:"fresh_policy"`
}

// Unresolved recognizes only the exact deliberate placeholder HOLD or absence.
func Unresolved(s string) bool { return s == "" || s == "HOLD" }
func (r *Report) Path(field, s string) bool {
	if Unresolved(s) {
		r.Add(field, Hold, "operator_input_missing")
		return false
	}
	if !filepath.IsAbs(s) || filepath.Clean(s) != s || s == "/" || strings.IndexByte(s, 0) >= 0 {
		r.Add(field, Fail, "invalid_path")
		return false
	}
	r.Add(field, Pass, "declared_absolute_path")
	return true
}
func overlaps(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}
func (r *Report) Match(field string, a, b any) {
	if s, ok := a.(string); ok && Unresolved(s) {
		r.Add(field, Hold, "unresolved_comparison")
		return
	}
	if s, ok := b.(string); ok && Unresolved(s) {
		r.Add(field, Hold, "unresolved_comparison")
		return
	}

	if !reflect.DeepEqual(a, b) {
		r.Add(field, Fail, "value_conflict")
	} else {
		r.Add(field, Pass, "declared_match")
	}
}
func read(path, field string, r *Report) []byte {
	if path == "" {
		r.Add(field, Fail, "explicit_file_required")
		return nil
	}
	b, err := configfile.Read(path)
	if err != nil {
		r.Add(field, Fail, "unsafe_or_unreadable_input")
		return nil
	}
	return b
}

func Check(ctx context.Context, in Inputs, p Product) Report {
	r := Report{Version: 1, Status: Pass, Scope: "explicit_files_only_no_environment_or_runtime_overrides", OperationalReadiness: "not_authorized", LiveFreshness: "not_checked", FullStateReconstruction: "not_performed", GenesisContents: "not_checked", HostAndPeerVerification: "not_performed", Findings: []Finding{}}
	if ctx.Err() != nil {
		r.Add("input", Fail, "cancelled")
		return r
	}
	app := read(in.App, "app", &r)
	native := read(in.Native, "native", &r)
	checkpoint := read(in.Checkpoint, "checkpoint", &r)
	decl := read(in.Declaration, "declaration", &r)
	var retention []byte
	if p.SeparateRetention {
		retention = read(in.Retention, "retention", &r)
	} else if in.Retention != "" {
		r.Add("retention", Fail, "unsupported_separate_retention_profile")
	}
	c, au, ae := appCopy(app, &r)
	appOK := ae == nil
	if !appOK {
		r.Add("app", Fail, "invalid_schema")
	}
	var d Declaration
	du, de := DecodeDeclaredJSON(decl, &d, "declaration", append(IdentityFields("chain", "anchor", "root"), declarationFields()...), &r)
	declOK := de == nil
	if !declOK {
		r.Add("declaration", Fail, "invalid_schema")
	}
	var m format.Manifest
	mu, me := DecodeDeclaredJSON(checkpoint, &m, "checkpoint", IdentityFields("Chain", "Ref", "Root"), &r)
	checkpointOK := me == nil
	if !checkpointOK {
		r.Add("checkpoint", Fail, "invalid_schema")
	}
	var n Native
	var observations []Native
	nativeOK := false
	if p.Decode == nil || p.Profile == "" {
		r.Add("profile", Fail, "unsupported_profile")
	} else {
		var err error
		n, err = p.Decode(native, retention, &r)
		nativeOK = err == nil
		if err != nil {
			r.Add("native", Fail, "invalid_schema_or_limits")
		}
	}
	if nativeOK {
		observations = append(observations, n)
	}
	if declOK {
		observations = append(observations, Native{Chain: d.Chain, Anchor: d.Anchor, Root: d.Root, Unknown: du})
	}
	if checkpointOK {
		observations = append(observations, Native{Chain: m.Chain, Anchor: m.Ref, Root: m.Root, Unknown: mu})
		if m.Mode == "genesis" {
			u := unknownIdentity()
			u["height"] = false
			observations = append(observations, Native{Anchor: execwire.Ref{Height: 0}, Unknown: u})
		}
	}
	if nativeOK && !validIdentity(n.Chain, n.Anchor, n.Unknown) {
		r.Add("native.identity", Fail, "invalid_identity")
	}
	if appOK {
		r.Match("app.database.postgres.max_conns", c.Database.Postgres.MaxConns, int32(4))
		r.Match("app.rpc.rate_limit", c.RPC.RateLimit, float64(5))
		// Validate a copy with neutral values only for absent credential inputs. No
		// environment loader is invoked and no normalization escapes this function.
		if Unresolved(c.Database.Postgres.DSN) {
			r.Add("app.database.postgres.dsn", Hold, "operator_input_missing")
			c.Database.Postgres.DSN = "postgres://offline.invalid/synthetic"
		}
		if Unresolved(c.RPC.Endpoint) {
			r.Add("app.rpc.endpoint", Hold, "operator_input_missing")
			c.RPC.Endpoint = "https://offline.invalid"
		}
		if err := appruntime.ValidateConfig(c); err != nil {
			r.Add("app", Fail, "production_validation_failed")
		} else {
			if len(au) > 0 {
				r.Add("app", Hold, "production_validation_incomplete")
			} else {
				r.Add("app", Pass, "production_validation_passed")
			}
		}
		e := c.Source.Execution
		if e != nil {
			u := unknownIdentity()
			u["height"] = false
			u["hash"] = au["hash"]
			u["root"] = au["root"]
			var hash, root execwire.Hash
			if hash.UnmarshalText([]byte(e.AnchorHash)) != nil {
				u["hash"] = true
			}
			if root.UnmarshalText([]byte(e.AnchorRoot)) != nil {
				u["root"] = true
			}
			observations = append(observations, Native{Anchor: execwire.Ref{Height: e.AnchorHeight, Hash: hash}, Root: root, Unknown: u})
		}
		if e != nil && e.RequireBlobSidecars {
			r.Add("app.source.execution.require_blob_sidecars", Hold, "external_blob_supply_not_checked")
		}
		if e == nil {
			r.Add("app.source.execution", Fail, "native_source_required")
		} else if nativeOK {
			r.MatchKnown("app.anchor_height", e.AnchorHeight, n.Anchor.Height, n.Unknown["height"])
			r.MatchKnown("app.anchor_hash", e.AnchorHash, hashText(n.Anchor.Hash), n.Unknown["hash"] || au["hash"])
			r.MatchKnown("app.anchor_root", e.AnchorRoot, hashText(n.Root), n.Unknown["root"] || au["root"])
			r.MatchKnown("app.socket", e.Socket, n.Socket, au["socket"])
			matchSelections(&r, e.RequiredStateProofs, n.StateProofs)
			if e.Consensus != nil {
				knownPolicy := true
				for _, v := range []struct{ field, value, expected string }{{"profile", e.Consensus.Profile, p.ConsensusProfile}, {"protocol", e.Consensus.Protocol, p.ConsensusProtocol}} {
					if Unresolved(v.value) {
						r.Add("app.consensus."+v.field, Hold, "operator_input_missing")
						knownPolicy = false
					} else if v.value != v.expected {
						r.Add("app.consensus."+v.field, Fail, "unsupported_consensus_profile")
						knownPolicy = false
					}
				}
				if Unresolved(e.Consensus.Parameters) {
					r.Add("app.consensus.parameters", Hold, "operator_input_missing")
					knownPolicy = false
				}

				if p.Consensus == nil {
					r.Add("app.consensus", Fail, "unsupported_consensus_profile")
				} else if knownPolicy {
					expected := n.Chain
					if n.Unknown["chain_id"] {
						expected.ID = ""
						r.Add("app.consensus.chain_id", Hold, "unresolved_comparison")
					}
					if n.Unknown["genesis"] {
						expected.Genesis = execwire.Hash{}
						r.Add("app.consensus.genesis", Hold, "unresolved_comparison")
					}
					v, err := p.Consensus(expected, *e.Consensus)
					if err != nil {
						r.Add("app.consensus", Fail, "invalid_consensus_policy")
					} else {
						anchor, err := v.AnchorPoint()
						if err != nil {
							r.Add("app.consensus", Fail, "invalid_consensus_anchor")
						} else {
							u := unknownIdentity()
							u["height"], u["hash"], u["root"] = false, false, false
							observations = append(observations, Native{Anchor: execwire.Ref{Height: anchor.Height, Hash: anchor.Hash}, Root: anchor.Root, Unknown: u})
							r.MatchKnown("app.consensus.anchor_height", anchor.Height, n.Anchor.Height, n.Unknown["height"])
							r.MatchKnown("app.consensus.anchor_hash", anchor.Hash, n.Anchor.Hash, n.Unknown["hash"])
							r.MatchKnown("app.consensus.anchor_root", anchor.Root, n.Root, n.Unknown["root"])
						}
					}
				}
			}
		}
	}
	if declOK {
		r.Match("declaration.profile", d.Profile, p.Profile)
		if !Unresolved(d.APFSIdentity) && !uuidPattern.MatchString(d.APFSIdentity) {
			r.Add("declaration.apfs_identity", Fail, "invalid_identity_syntax")
		}
		if !Unresolved(d.CheckpointAuthority) && !authorityPattern.MatchString(d.CheckpointAuthority) {
			r.Add("declaration.checkpoint_authority", Fail, "invalid_authority_syntax")
		}

		if !validIdentity(d.Chain, d.Anchor, du) || d.Chain.Genesis == (execwire.Hash{}) || !du["network"] && d.NetworkID == 0 {
			r.Add("declaration.identity", Fail, "invalid_identity")
		}
		if p.Identity != nil {
			p.Identity(d.Chain, d.NetworkID, du, &r)
		}

		r.Match("declaration.part_transactions", d.PartTransactions, 2)
		r.Match("declaration.api_reservation", d.APIReservation, 1)
		r.Match("declaration.logical_writers", d.LogicalWriters, 7)
		r.Match("declaration.fresh_policy", d.FreshPolicy, "new_uncached_canonical_post_response_generation_fence_commit")
		height := d.Anchor.Height
		knownHeight := !du["height"]
		if !knownHeight && nativeOK && !n.Unknown["height"] {
			height = n.Anchor.Height
			knownHeight = true
		}
		if !knownHeight && checkpointOK && !mu["height"] {
			height = m.Ref.Height
			knownHeight = true
		}
		if !knownHeight && appOK && c.Source.Execution != nil {
			height = c.Source.Execution.AnchorHeight
			knownHeight = true
		}
		if knownHeight && (height >= math.MaxInt64 || !du["floor"] && d.RetainedFloor < height || !du["start"] && d.StartHeight <= height) || !du["generation"] && d.Anchor.Generation != 1 || !du["floor"] && !du["start"] && d.RetainedFloor >= d.StartHeight {
			r.Add("declaration.boundaries", Fail, "invalid_start_anchor_floor")
		}
		switch d.StartMode {
		case "initial":
			if knownHeight && (!du["start"] && d.StartHeight != height+1 || !du["floor"] && d.RetainedFloor != height) {
				r.Add("declaration.start_mode", Fail, "invalid_initial_boundary")
			}
		case "resume":
			r.Add("declaration.start_mode", Pass, "declared_resume_only_not_store_verified")
		case "", "HOLD":
			r.Add("declaration.start_mode", Hold, "operator_input_missing")
		default:
			r.Add("declaration.start_mode", Fail, "unsupported_start_mode")
		}

		for _, v := range []struct{ field, value string }{{"declaration.apfs_identity", d.APFSIdentity}, {"declaration.checkpoint_authority", d.CheckpointAuthority}} {
			if Unresolved(v.value) {
				r.Add(v.field, Hold, "operator_input_missing")
			} else {
				r.Add(v.field, Pass, "declared_only_unverified")
			}
		}
		if Unresolved(d.Enode) {
			r.Add("declaration.enode", Hold, "operator_input_missing")
		} else if !ValidEnode(d.Enode) {
			r.Add("declaration.enode", Fail, "invalid_enode")
		} else {
			r.Add("declaration.enode", Pass, "declared_only_unverified")
		}
		if nativeOK {
			if p.Identity != nil {
				nu := Unknown{}
				for k, v := range n.Unknown {
					nu[k] = v
				}
				nu["network"] = du["network"]
				p.Identity(n.Chain, d.NetworkID, nu, &r)
			}

			r.ChainMatch("declaration.chain", d.Chain, n.Chain, du, n.Unknown)
			if n.NetworkID != 0 {
				r.MatchKnown("declaration.network_id", d.NetworkID, n.NetworkID, n.Unknown["network_id"] || du["network"])
			}
			if p.PeerMembership && !Unresolved(d.Enode) {
				found := false
				unresolvedPeers := len(n.Peers) == 0
				for _, peer := range n.Peers {
					unresolvedPeers = unresolvedPeers || Unresolved(peer)
					found = found || peer == d.Enode
				}
				if !found && !unresolvedPeers {
					r.Add("declaration.enode", Fail, "peer_not_in_executor")
				}
			}

			r.AnchorMatch("declaration.anchor", d.Anchor, n.Anchor, du, n.Unknown)
			r.MatchKnown("declaration.root", d.Root, n.Root, du["root"] || n.Unknown["root"])
			reserve := n.Reserve
			if reserve == 0 {
				reserve = 1 << 30
			}
			invalidCapacity := !du["native_bytes"] && d.NativeBytes == 0 || !du["indexer_bytes"] && d.IndexerBytes == 0 || !du["checkpoint_bytes"] && d.CheckpointBytes == 0 || !du["scratch_bytes"] && d.ScratchBytes == 0
			if !du["backup_bytes"] && !fits(d.BackupBytes, d.NativeBytes, n.SpoolBytes, d.IndexerBytes) {
				invalidCapacity = true
			}
			if !du["free_bytes"] && !fits(d.FreeBytes, d.NativeBytes, d.NativeBytes, reserve, n.SpoolBytes, d.IndexerBytes, d.CheckpointBytes, d.ScratchBytes, d.BackupBytes) {
				invalidCapacity = true
			}
			if invalidCapacity {
				r.Add("declaration.capacity", Fail, "insufficient_declared_headroom")
			} else {
				unknownCapacity := false
				for _, key := range []string{"native_bytes", "indexer_bytes", "checkpoint_bytes", "scratch_bytes", "backup_bytes", "free_bytes"} {
					unknownCapacity = unknownCapacity || du[key]
				}
				if unknownCapacity {
					r.Add("declaration.capacity", Hold, "declared_headroom_incomplete")
				} else {
					r.Add("declaration.capacity", Pass, "declared_headroom_only")
				}
			}

			if Unresolved(n.Socket) && appOK && c.Source.Execution != nil && !au["socket"] {
				for i := range n.ExtraPaths {
					if n.ExtraPaths[i].Field == "native.socket" {
						n.ExtraPaths[i].Value = c.Source.Execution.Socket
					}
				}
			}
			paths := []DeclaredPath{{"native.data", n.Data}, {"native.spool", n.Spool}, {"native.scratch", n.Scratch}, {"declaration.checkpoint_path", d.CheckpointPath}, {"declaration.pg_data", d.PGData}}
			if !p.CollectorScratch {
				paths = append(paths[:2], paths[3:]...)
			}
			paths = append(paths, n.ExtraPaths...)
			valid := make([]bool, len(paths))
			for i, v := range paths {
				valid[i] = r.Path(v.Field, v.Value)
			}
			for i, a := range paths {
				for j := i + 1; j < len(paths); j++ {
					if valid[i] && valid[j] && overlaps(a.Value, paths[j].Value) {
						r.Add(a.Field+"/"+paths[j].Field, Fail, "path_overlap")
					}
				}
			}
		}
		if appOK {
			r.MatchKnown("app.start_height", c.Indexer.StartHeight, d.StartHeight, du["start"])
		}
	}
	if checkpointOK {
		if p.Identity != nil {
			cu := Unknown{}
			for k, v := range mu {
				cu[k] = v
			}
			cu["network"] = !declOK || du["network"]
			p.Identity(m.Chain, d.NetworkID, cu, &r)
		}
		if !validIdentity(m.Chain, m.Ref, mu) {
			r.Add("checkpoint.identity", Fail, "invalid_identity")
		}
		if len(m.Header) > execwire.MaxHeaderBytes {
			r.Add("checkpoint.header", Fail, "header_limit_exceeded")
		} else if len(m.Header) > 0 {
			if p.HeaderCodec == nil {
				r.Add("checkpoint.header", Fail, "unsupported_header_profile")
			} else {
				h, err := p.HeaderCodec.DecodeHeaderRLP(m.Header)
				if err != nil {
					r.Add("checkpoint.header", Fail, "invalid_header")
				} else {
					u := unknownIdentity()
					u["height"], u["hash"], u["root"] = false, false, false
					observations = append(observations, Native{Anchor: execwire.Ref{Height: h.Number, Hash: execwire.Hash(h.Hash)}, Root: execwire.Hash(h.StateRoot), Unknown: u})
					r.MatchKnown("checkpoint.header.height", h.Number, m.Ref.Height, mu["height"])
					r.MatchKnown("checkpoint.header.hash", execwire.Hash(h.Hash), m.Ref.Hash, mu["hash"])
					r.MatchKnown("checkpoint.header.root", execwire.Hash(h.StateRoot), m.Root, mu["root"])
				}
			}
		}

		if m.Version != 1 || m.Mode != "genesis" && m.Mode != "checkpoint" || m.Coverage != "full" || m.Ref.Generation != 1 || m.Ref.Height >= math.MaxInt64 || m.Root == (execwire.Hash{}) || m.Chain.Genesis == (execwire.Hash{}) || m.Ref.Hash == (execwire.Hash{}) || m.Mode == "genesis" && (!mu["height"] && m.Ref.Height != 0 || !mu["hash"] && !mu["genesis"] && m.Ref.Hash != m.Chain.Genesis) {
			r.Add("checkpoint", Fail, "invalid_full_baseline_metadata")
		}
		if nativeOK {
			r.ChainMatch("checkpoint.chain", m.Chain, n.Chain, mu, n.Unknown)
			r.AnchorMatch("checkpoint.anchor", m.Ref, n.Anchor, mu, n.Unknown)
			r.MatchKnown("checkpoint.root", m.Root, n.Root, mu["root"] || n.Unknown["root"])
		}
	}
	// Compare the independently known files even when the native identity is
	// unresolved. A placeholder must never mask conflicts between other inputs.
	if declOK && checkpointOK {
		r.ChainMatch("declaration/checkpoint.chain", d.Chain, m.Chain, du, mu)
		r.AnchorMatch("declaration/checkpoint.anchor", d.Anchor, m.Ref, du, mu)
		r.MatchKnown("declaration/checkpoint.root", d.Root, m.Root, du["root"] || mu["root"])
	}
	if appOK && c.Source.Execution != nil {
		e := c.Source.Execution
		if declOK {
			r.MatchKnown("app/declaration.height", e.AnchorHeight, d.Anchor.Height, du["height"])
			r.MatchKnown("app/declaration.hash", e.AnchorHash, hashText(d.Anchor.Hash), au["hash"] || du["hash"])
			r.MatchKnown("app/declaration.root", e.AnchorRoot, hashText(d.Root), au["root"] || du["root"])
		}
		if checkpointOK {
			r.MatchKnown("app/checkpoint.height", e.AnchorHeight, m.Ref.Height, mu["height"])
			r.MatchKnown("app/checkpoint.hash", e.AnchorHash, hashText(m.Ref.Hash), au["hash"] || mu["hash"])
			r.MatchKnown("app/checkpoint.root", e.AnchorRoot, hashText(m.Root), au["root"] || mu["root"])
		}
	}
	merged := checkKnownIdentities(&r, observations)
	if p.Identity != nil {
		merged.Unknown["network"] = !declOK || du["network"]
		p.Identity(merged.Chain, d.NetworkID, merged.Unknown, &r)
	}
	if ctx.Err() != nil {
		r.Add("input", Fail, "cancelled")
	}
	return r
}
func ValidEnode(s string) bool { _, err := enode.ParseV4(s); return err == nil }

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var authorityPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,127}$`)

// Run emits exactly one bounded report; flags, parser errors and cancellation
// never leak raw input values. No App, store, subprocess or network is opened.
func Run(ctx context.Context, args []string, out io.Writer, p Product) int {
	in, err := parseArgs(args)
	var r Report
	if err != nil {
		r = Report{Version: 1, Status: Fail, OperationalReadiness: "not_authorized", LiveFreshness: "not_checked", FullStateReconstruction: "not_performed", GenesisContents: "not_checked", HostAndPeerVerification: "not_performed", Scope: "explicit_files_only_no_environment_or_runtime_overrides", Findings: []Finding{{"cli", Fail, "invalid_arguments"}}}
	} else {
		r = Check(ctx, in, p)
	}
	if ctx.Err() != nil {
		r.Add("input", Fail, "cancelled")
	}
	b, err := json.Marshal(r)
	if err != nil || len(b) > 64<<10 {
		return 1
	}
	b = append(b, '\n')
	if written, err := out.Write(b); err != nil || written != len(b) {
		return 1
	}
	return r.ExitCode()
}
func parseArgs(args []string) (Inputs, error) {
	var in Inputs
	seen := map[string]bool{}
	for i := 0; i < len(args); i += 2 {
		if i+1 >= len(args) || seen[args[i]] {
			return in, fmt.Errorf("arguments")
		}
		seen[args[i]] = true
		switch args[i] {
		case "--app":
			in.App = args[i+1]
		case "--native":
			in.Native = args[i+1]
		case "--checkpoint":
			in.Checkpoint = args[i+1]
		case "--retention":
			in.Retention = args[i+1]
		case "--declaration":
			in.Declaration = args[i+1]
		default:
			return in, fmt.Errorf("arguments")
		}
	}
	return in, nil
}

func hashText(h execwire.Hash) string { b, _ := h.MarshalText(); return string(b) }

func fits(total uint64, parts ...uint64) bool {
	for _, part := range parts {
		if part > total {
			return false
		}
		total -= part
	}
	return true
}

func matchSelections(r *Report, a, b *stateproof.Selection) {
	if a == nil || b == nil {
		r.Match("app.state_proofs", a, b)
		return
	}
	_, ad, ae := a.Normalize()
	_, bd, be := b.Normalize()
	if ae != nil || be != nil {
		r.Add("app.state_proofs", Fail, "invalid_state_proof_policy")
		return
	}
	r.Match("app.state_proofs", ad, bd)
}

// Validate only known identity relationships using the production wire rules.
// Unknown fields in this detached copy cannot create a false genesis conflict.
func validIdentity(chain execwire.Chain, anchor execwire.Ref, u Unknown) bool {
	if u["height"] {
		anchor.Height = 1
	}
	if anchor.Height == 0 {
		if u["genesis"] {
			chain.Genesis = anchor.Hash
		}
		if u["hash"] {
			anchor.Hash = chain.Genesis
		}
	}
	return execwire.Encode(io.Discard, execwire.Frame{Kind: execwire.Hello, Header: execwire.Header{Version: execwire.Version, Chain: chain, Ref: anchor}}) == nil
}
