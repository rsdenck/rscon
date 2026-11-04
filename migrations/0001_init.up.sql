-- RSCON initial schema.
-- IDs are application-generated UUIDv4 strings. Timestamps are UTC.

CREATE TABLE identity_providers (
    id          text PRIMARY KEY,
    name        text NOT NULL UNIQUE,
    kind        text NOT NULL CHECK (kind IN ('oidc', 'ldap', 'ad', 'local')),
    issuer      text NOT NULL DEFAULT '',
    config      jsonb NOT NULL DEFAULT '{}'::jsonb,
    enabled     boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE environments (
    id          text PRIMARY KEY,
    name        text NOT NULL UNIQUE,
    description text NOT NULL DEFAULT '',
    critical    boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id               text PRIMARY KEY,
    username         text NOT NULL,
    email            text,
    display_name     text NOT NULL DEFAULT '',
    password_hash    text,
    status           text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled', 'locked')),
    mfa_enrolled     boolean NOT NULL DEFAULT false,
    idp_id           text REFERENCES identity_providers(id) ON DELETE SET NULL,
    last_login_at    timestamptz,
    failed_logins    integer NOT NULL DEFAULT 0,
    locked_until     timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    deleted_at       timestamptz
);
CREATE UNIQUE INDEX users_username_unique ON users (lower(username)) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX users_email_unique ON users (lower(email)) WHERE email IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX users_status_idx ON users (status) WHERE deleted_at IS NULL;

CREATE TABLE groups (
    id          text PRIMARY KEY,
    name        text NOT NULL UNIQUE,
    description text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_groups (
    user_id  text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id text NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, group_id)
);

CREATE TABLE roles (
    id          text PRIMARY KEY,
    name        text NOT NULL UNIQUE,
    description text NOT NULL DEFAULT '',
    system      boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE permissions (
    action      text PRIMARY KEY,
    description text NOT NULL DEFAULT ''
);

CREATE TABLE role_permissions (
    role_id text NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    action  text NOT NULL REFERENCES permissions(action) ON DELETE CASCADE,
    PRIMARY KEY (role_id, action)
);

CREATE TABLE role_bindings (
    id           text PRIMARY KEY,
    role_id      text NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    subject_type text NOT NULL CHECK (subject_type IN ('user', 'group')),
    subject_id   text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (role_id, subject_type, subject_id)
);
CREATE INDEX role_bindings_subject_idx ON role_bindings (subject_type, subject_id);

CREATE TABLE server_groups (
    id          text PRIMARY KEY,
    name        text NOT NULL UNIQUE,
    description text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE servers (
    id                   text PRIMARY KEY,
    name                 text NOT NULL,
    hostname             text NOT NULL DEFAULT '',
    address              text NOT NULL,
    port                 integer NOT NULL DEFAULT 22 CHECK (port > 0 AND port < 65536),
    environment_id       text NOT NULL REFERENCES environments(id),
    os_family            text NOT NULL DEFAULT '',
    location             text NOT NULL DEFAULT '',
    tags                 text[] NOT NULL DEFAULT '{}',
    default_account      text NOT NULL DEFAULT '',
    host_key_fingerprint text NOT NULL DEFAULT '',
    enabled              boolean NOT NULL DEFAULT true,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),
    deleted_at           timestamptz
);
CREATE UNIQUE INDEX servers_name_unique ON servers (lower(name)) WHERE deleted_at IS NULL;
CREATE INDEX servers_environment_idx ON servers (environment_id) WHERE deleted_at IS NULL;

CREATE TABLE server_group_members (
    server_id text NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    group_id  text NOT NULL REFERENCES server_groups(id) ON DELETE CASCADE,
    PRIMARY KEY (server_id, group_id)
);

CREATE TABLE access_policies (
    id                  text PRIMARY KEY,
    name                text NOT NULL UNIQUE,
    description         text NOT NULL DEFAULT '',
    priority            integer NOT NULL DEFAULT 100,
    effect              text NOT NULL CHECK (effect IN ('allow', 'deny')),
    subjects            text[] NOT NULL DEFAULT '{}',
    actions             text[] NOT NULL DEFAULT '{}',
    server_ids          text[] NOT NULL DEFAULT '{}',
    server_group_ids    text[] NOT NULL DEFAULT '{}',
    environment_ids     text[] NOT NULL DEFAULT '{}',
    accounts            text[] NOT NULL DEFAULT '{}',
    allowed_channels    text[] NOT NULL DEFAULT '{}',
    require_approval    boolean NOT NULL DEFAULT false,
    approvals_required  integer NOT NULL DEFAULT 0 CHECK (approvals_required >= 0),
    require_mfa         boolean NOT NULL DEFAULT false,
    max_duration_seconds integer NOT NULL DEFAULT 0 CHECK (max_duration_seconds >= 0),
    time_windows        jsonb NOT NULL DEFAULT '[]'::jsonb,
    enabled             boolean NOT NULL DEFAULT true,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX access_policies_enabled_idx ON access_policies (enabled, priority);

CREATE TABLE access_requests (
    id                text PRIMARY KEY,
    requester_id      text NOT NULL REFERENCES users(id),
    server_id         text REFERENCES servers(id),
    server_group_id   text REFERENCES server_groups(id),
    environment_id    text REFERENCES environments(id),
    account           text NOT NULL,
    reason            text NOT NULL,
    ticket            text NOT NULL DEFAULT '',
    requested_seconds integer NOT NULL CHECK (requested_seconds > 0),
    status            text NOT NULL DEFAULT 'pending'
                      CHECK (status IN ('pending', 'approved', 'denied', 'expired', 'canceled')),
    starts_at         timestamptz,
    ends_at           timestamptz,
    approvals_required integer NOT NULL DEFAULT 1,
    approvals_count   integer NOT NULL DEFAULT 0,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    decided_at        timestamptz,
    CHECK (server_id IS NOT NULL OR server_group_id IS NOT NULL OR environment_id IS NOT NULL)
);
CREATE INDEX access_requests_requester_idx ON access_requests (requester_id, created_at DESC);
CREATE INDEX access_requests_status_idx ON access_requests (status, created_at DESC);
CREATE INDEX access_requests_grant_idx ON access_requests (requester_id, status, starts_at, ends_at);

CREATE TABLE access_approvals (
    id          text PRIMARY KEY,
    request_id  text NOT NULL REFERENCES access_requests(id) ON DELETE CASCADE,
    approver_id text NOT NULL REFERENCES users(id),
    decision    text NOT NULL CHECK (decision IN ('approve', 'deny')),
    comment     text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (request_id, approver_id)
);

CREATE TABLE ssh_certificates (
    id                   text PRIMARY KEY,
    serial               bigint NOT NULL UNIQUE,
    user_id              text NOT NULL REFERENCES users(id),
    server_id            text NOT NULL REFERENCES servers(id),
    account              text NOT NULL,
    key_id               text NOT NULL,
    public_key_fingerprint text NOT NULL DEFAULT '',
    cert_pem             text NOT NULL,
    ca_name              text NOT NULL DEFAULT '',
    valid_after          timestamptz NOT NULL,
    valid_before         timestamptz NOT NULL,
    revoked_at           timestamptz,
    revoked_reason       text NOT NULL DEFAULT '',
    created_at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ssh_certificates_user_idx ON ssh_certificates (user_id, created_at DESC);
CREATE INDEX ssh_certificates_active_idx ON ssh_certificates (serial) WHERE revoked_at IS NULL;

CREATE TABLE sessions (
    id                text PRIMARY KEY,
    user_id           text NOT NULL REFERENCES users(id),
    server_id         text NOT NULL REFERENCES servers(id),
    account           text NOT NULL,
    source_ip         text NOT NULL,
    auth_method       text NOT NULL,
    certificate_id    text REFERENCES ssh_certificates(id) ON DELETE SET NULL,
    gateway           text NOT NULL,
    status            text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'closed', 'failed', 'interrupted')),
    command           text NOT NULL DEFAULT '',
    started_at        timestamptz NOT NULL DEFAULT now(),
    ended_at          timestamptz,
    duration_seconds  integer NOT NULL DEFAULT 0,
    termination_reason text NOT NULL DEFAULT '',
    exit_status       integer,
    recording_id      text,
    terminate_requested boolean NOT NULL DEFAULT false,
    updated_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_active_idx ON sessions (status, started_at DESC) WHERE status = 'active';
CREATE INDEX sessions_user_idx ON sessions (user_id, started_at DESC);
CREATE INDEX sessions_server_idx ON sessions (server_id, started_at DESC);
CREATE INDEX sessions_terminate_idx ON sessions (terminate_requested) WHERE terminate_requested;

CREATE TABLE session_recordings (
    id         text PRIMARY KEY,
    session_id text NOT NULL REFERENCES sessions(id),
    backend    text NOT NULL,
    object_key text NOT NULL,
    format     text NOT NULL DEFAULT 'asciicast-v2',
    size_bytes bigint NOT NULL DEFAULT 0,
    sha256     text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX session_recordings_session_idx ON session_recordings (session_id);

CREATE TABLE authentication_events (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id     text REFERENCES users(id) ON DELETE SET NULL,
    username    text NOT NULL DEFAULT '',
    method      text NOT NULL,
    result      text NOT NULL,
    source_ip   text NOT NULL DEFAULT '',
    reason      text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX authentication_events_time_idx ON authentication_events (created_at DESC);

CREATE TABLE audit_events (
    id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id       text NOT NULL UNIQUE,
    category       text NOT NULL,
    action         text NOT NULL,
    actor_id       text,
    actor_name     text NOT NULL DEFAULT '',
    result         text NOT NULL,
    resource_type  text NOT NULL DEFAULT '',
    resource_id    text NOT NULL DEFAULT '',
    source_ip      text NOT NULL DEFAULT '',
    correlation_id text NOT NULL DEFAULT '',
    message        text NOT NULL DEFAULT '',
    details        jsonb NOT NULL DEFAULT '{}'::jsonb,
    prev_hash      text NOT NULL,
    hash           text NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_events_time_idx ON audit_events (created_at DESC);
CREATE INDEX audit_events_actor_idx ON audit_events (actor_id, created_at DESC);
CREATE INDEX audit_events_category_idx ON audit_events (category, created_at DESC);

CREATE TABLE system_settings (
    key        text PRIMARY KEY,
    value      jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE api_tokens (
    id         text PRIMARY KEY,
    user_id    text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind       text NOT NULL CHECK (kind IN ('session', 'api')),
    token_hash text NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    last_used_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX api_tokens_user_idx ON api_tokens (user_id);

CREATE TABLE user_public_keys (
    id          text PRIMARY KEY,
    user_id     text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        text NOT NULL DEFAULT '',
    public_key  text NOT NULL,
    fingerprint text NOT NULL UNIQUE,
    created_at  timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz
);
CREATE INDEX user_public_keys_user_idx ON user_public_keys (user_id);
