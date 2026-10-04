package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"time"

	"github.com/stainedhead/agent-cli-core/audit"
	"github.com/stainedhead/agent-cli-core/auth"
	"github.com/stainedhead/agent-cli-core/selftest"

	"github.com/stainedhead/outlook-cli/internal/adapter/auditlog"
	"github.com/stainedhead/outlook-cli/internal/adapter/graph"
	"github.com/stainedhead/outlook-cli/internal/adapter/ledger"
	"github.com/stainedhead/outlook-cli/internal/adapter/policyfile"
	"github.com/stainedhead/outlook-cli/internal/adapter/selftestcfg"
	"github.com/stainedhead/outlook-cli/internal/domain"
	"github.com/stainedhead/outlook-cli/internal/usecase"
)

// Provider name the credential daemon knows the Graph credential by
// (agent-okta-d PRD section 7.5).
const graphProvider = "msgraph"

// Defaults and environment variables of the composition root.
//
// ASSUMPTION (unverified): the PRD fixes only the policy path
// (/etc/agent-cli/outlook.policy.yaml). The environment variable names, the
// ledger file name and the run-id scheme below are this build's choices.
const (
	defaultPolicyPath = "/etc/agent-cli/outlook.policy.yaml"
	envPolicy         = "OUTLOOK_POLICY"
	envAgentID        = "AGENT_ID"
	envRunID          = "AGENT_RUN_ID"
	ledgerFileName    = "outlook.idempotency.json"
	remediation       = "a human must run: agent-okta-d enroll msgraph"
)

// appConfig holds everything the composition root can be pointed elsewhere
// for. Production uses prodConfig(); integration tests inject fakes (never a
// real network).
type appConfig struct {
	PolicyPath string
	// PolicyOpts are policyfile load options (tests use AllowUntrusted because
	// a temp directory is owned by the test user; a release build passes none,
	// FR-R2).
	PolicyOpts []policyfile.Option
	AgentID    string
	RunID      string
	Daemon     auth.DaemonClient
	// GraphBaseURL empty means https://graph.microsoft.com/v1.0.
	GraphBaseURL string
	// Graph carries HTTP retry tuning; zero in production.
	Graph graph.Config
	Clock usecase.Clock
	// LedgerPath empty means a file next to the audit log.
	LedgerPath string
}

// staticPolicy serves the policy already loaded (and validated) by assemble.
type staticPolicy struct{ p domain.Policy }

func (s staticPolicy) Policy(context.Context) (domain.Policy, error) { return s.p, nil }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now().UTC() }

func prodConfig() appConfig {
	c := appConfig{
		PolicyPath: defaultPolicyPath,
		AgentID:    os.Getenv(envAgentID),
		RunID:      os.Getenv(envRunID),
		Daemon:     newDaemonClient(),
		Clock:      systemClock{},
		PolicyOpts: devPolicyOpts(os.Getenv),
	}
	// OUTLOOK_POLICY may point elsewhere, but the target must still pass the
	// ownership check in policyfile.Load: an agent-authored file is refused
	// whatever its mode (FR-R2).
	if p := os.Getenv(envPolicy); p != "" {
		c.PolicyPath = p
	}
	return c
}

// app is the assembled object graph.
type app struct {
	cmds   usecase.Commands
	policy domain.Policy
	audit  *auditlog.Sink
}

func (a *app) close() { _ = a.audit.Close() }

func newRunID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "run-" + time.Now().UTC().Format("20060102T150405")
	}
	return "run-" + hex.EncodeToString(b)
}

// assemble builds every adapter and the use cases. It loads the policy first:
// a policy that cannot be loaded (missing, invalid, agent-writable) stops the
// run before any adapter is built.
func assemble(ctx context.Context, cfg appConfig) (*app, error) {
	pol, err := policyfile.NewProvider(cfg.PolicyPath, cfg.PolicyOpts...).Policy(ctx)
	if err != nil {
		return nil, err
	}
	if cfg.AgentID == "" {
		cfg.AgentID = pol.Profile
	}
	if cfg.RunID == "" {
		cfg.RunID = newRunID()
	}
	if cfg.Clock == nil {
		cfg.Clock = systemClock{}
	}

	src, err := auth.NewDaemonTokenSource(cfg.Daemon, graphProvider, auth.WithRemediation(remediation))
	if err != nil {
		return nil, domain.NewGeneral("credential source cannot be built").WithCause(err)
	}
	ledgerPath := cfg.LedgerPath
	if ledgerPath == "" {
		ledgerPath = filepath.Join(filepath.Dir(pol.AuditPath), ledgerFileName)
	}

	gcfg := cfg.Graph
	gcfg.Refresher = auth.NewAuthorizer(src)
	if cfg.GraphBaseURL != "" {
		gcfg.BaseURL = cfg.GraphBaseURL
	}
	// FR-R1: signed page tokens need a per-install key next to the ledger.
	gcfg.PageTokenKey = newPageKeyProvider(pageKeyPath(ledgerPath))
	gc, err := graph.New(gcfg)
	if err != nil {
		return nil, err
	}

	led, err := ledger.New(ledger.Config{Path: ledgerPath})
	if err != nil {
		return nil, err
	}

	// Block: a failed audit write fails the command, so no write goes
	// unrecorded. Reads are also blocked; an unwritable audit log is a
	// configuration fault worth surfacing.
	sink, err := auditlog.Open(auditlog.Config{
		Path: pol.AuditPath, AgentID: cfg.AgentID, RunID: cfg.RunID,
		Clock: cfg.Clock, FailureMode: audit.Block,
	})
	if err != nil {
		return nil, err
	}

	filters, err := policyfile.Filters(pol.Send.ContentFilters)
	if err != nil {
		_ = sink.Close()
		return nil, err
	}
	deps := usecase.Deps{
		Reader: gc, Writer: gc, Probe: gc,
		Ledger:     led,
		Policy:     staticPolicy{pol},
		Filters:    filters,
		Audit:      sink,
		Clock:      cfg.Clock,
		Run:        usecase.RunInfo{AgentID: cfg.AgentID, RunID: cfg.RunID},
		PolicyPath: cfg.PolicyPath,
	}
	if pol.Read.Attachments.Download {
		q, qerr := ledger.NewQuarantine(pol.Read.Attachments.OutDir)
		if qerr != nil {
			_ = sink.Close()
			return nil, qerr
		}
		deps.Quarantine = q
	}
	return &app{cmds: usecase.New(deps), policy: pol, audit: sink}, nil
}

// commandsFor returns the lazy use-case constructor the CLI calls. The audit
// file is held open for the (short) life of the process.
func commandsFor(cfg appConfig) func(context.Context) (usecase.Commands, error) {
	return func(ctx context.Context) (usecase.Commands, error) {
		a, err := assemble(ctx, cfg)
		if err != nil {
			return nil, err
		}
		return a.cmds, nil
	}
}

// selftestFor returns the `outlook selftest` runner. Every write row is a
// dry-run request, so nothing is ever sent.
func selftestFor(cfg appConfig) func(context.Context) (selftest.Result, error) {
	return func(ctx context.Context) (selftest.Result, error) {
		a, err := assemble(ctx, cfg)
		if err != nil {
			return selftest.Result{}, err
		}
		defer a.close()
		return selftestcfg.Runner(a.cmds, a.policy, false).Run(ctx)
	}
}
