DELETE FROM system_settings WHERE key = 'schema_version';
DELETE FROM environments WHERE id IN (
    'env-development', 'env-testing', 'env-staging', 'env-production',
    'env-infrastructure', 'env-security', 'env-disaster-recovery'
);
DELETE FROM role_permissions WHERE role_id LIKE 'role-%';
DELETE FROM roles WHERE system = true AND id LIKE 'role-%';
DELETE FROM permissions WHERE action IN (
    '*', 'ssh.connect', 'certificate.issue', 'certificate.read', 'certificate.revoke',
    'server.read', 'server.manage', 'user.read', 'user.manage', 'group.read',
    'group.manage', 'role.read', 'role.manage', 'policy.read', 'policy.manage',
    'access.request', 'access.approve', 'access.read', 'session.read',
    'session.terminate', 'recording.read', 'audit.read', 'vault.read',
    'environment.manage'
);
