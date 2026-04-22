# Security Baseline

Global security rules that apply across all projects and domains.

## Rules

### Rule: GSEC-001

- Owner layer: Global
- Scope: all projects and domains
- Stability: core
- Status: active
- Directive: Never commit secrets, API keys, tokens, passwords, or credentials to version control. Use environment variables or secret managers.
- Rationale: Secrets in code are the most common source of security incidents in repositories.
- Conflict handling: Project-level examples may document how secrets are supplied, but no project rule may permit committed secrets.
- Example: Store credentials in environment variables, CI secrets, or managed secret stores.
- Non-example: Commit `.env`, API tokens, private keys, or credentials JSON files.

### Rule: GSEC-002

- Owner layer: Global
- Scope: all projects and domains
- Stability: core
- Status: active
- Directive: Never execute shell commands, SQL queries, or code constructed from unvalidated external input.
- Rationale: Prevents injection attacks (command injection, SQL injection, code injection).
- Conflict handling: Domain or project rules may define approved sanitization or parameterization patterns, but must not allow raw unvalidated execution paths.
- Example: Use parameterized SQL and validated allowlists for shell subcommands.
- Non-example: Concatenate user-controlled strings into SQL, shell, or eval-like execution.

### Rule: GSEC-003

- Owner layer: Global
- Scope: state-changing operations and access to protected data
- Stability: core
- Status: active
- Directive: Every endpoint or operation that accesses user data or modifies state must verify the caller's identity and permissions.
- Rationale: Prevents unauthorized access and privilege escalation.
- Conflict handling: Project rules may narrow the auth model or permission scheme, but must not remove authentication and authorization checks for protected actions.
- Example: Check session identity and role/ownership before reading private records or mutating state.
- Non-example: Rely on client-side checks alone or assume internal callers are always trusted.

### Rule: GSEC-004

- Owner layer: Global
- Scope: all dependency management and build pipelines
- Stability: core
- Status: active
- Directive: Pin all third-party dependencies to specific versions and verify checksums or lock files. Never auto-upgrade dependencies in production pipelines without a review step.
- Rationale: Supply chain attacks (OWASP A06) exploit unpinned or automatically upgraded dependencies to inject malicious code.
- Conflict handling: Project rules may define the approved update cadence (e.g., monthly automated PRs), but must not remove version pinning or checksum verification.
- Example: Commit `package-lock.json`, `go.sum`, `Pipfile.lock`, or equivalent. CI fails if the lock file is missing or out of sync.
- Non-example: Use `*` or `latest` version constraints in production; run `npm update` in a CI pipeline without a diff review gate.

### Rule: GSEC-005

- Owner layer: Global
- Scope: all API endpoints and user-facing input handling
- Stability: core
- Status: active
- Directive: Validate, sanitize, and reject unexpected input at every system boundary. Define an explicit allowlist of acceptable values where possible; do not rely on a blocklist alone.
- Rationale: Covers OWASP A03 (Injection) and A01 (Broken Access Control) — most injection attacks exploit missing boundary validation.
- Conflict handling: Domain rules may define the specific validation library or schema format, but must not bypass boundary validation.
- Example: Validate request body against a typed schema; reject unknown fields; enforce string length and format constraints before any processing.
- Non-example: Pass raw user-supplied strings to database queries, shell commands, file paths, or template engines without validation.

### Rule: GSEC-006

- Owner layer: Global
- Scope: all configuration, feature flags, and default settings
- Stability: core
- Status: active
- Directive: Default to the most restrictive setting. Enable capabilities, permissions, and access explicitly rather than disabling them after the fact.
- Rationale: Covers OWASP A05 (Security Misconfiguration) — permissive defaults are frequently left unchanged in production.
- Conflict handling: Project rules may define specific default values, but must document and justify any default that is less restrictive than the minimum required.
- Example: New API endpoints are unauthenticated-by-default off; CORS is deny-all by default; debug endpoints require explicit opt-in.
- Non-example: Ship with `DEBUG=true`, open CORS (`*`), or admin endpoints enabled by default in production configuration.

### Rule: GSEC-007

- Owner layer: Global
- Scope: all logging, monitoring, and error reporting
- Stability: core
- Status: active
- Directive: Log security-relevant events (auth failures, permission denials, input validation rejections) at an appropriate severity level. Never log secrets, tokens, PII, or full request bodies containing sensitive data.
- Rationale: Covers OWASP A09 (Security Logging and Monitoring Failures) — missing logs prevent incident detection; over-logging leaks sensitive data.
- Conflict handling: Project rules may define the log format and retention policy, but must not permit logging credentials or suppress security event logging entirely.
- Example: Log failed login attempts with timestamp, IP, and username (not password). Mask card numbers before logging payment events.
- Non-example: Log the full Authorization header value; suppress all auth error logs to "reduce noise"; print API keys in stack traces.
