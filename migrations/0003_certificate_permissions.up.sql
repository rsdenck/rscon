-- Grant certificate issuance to roles that need to obtain short-lived SSH
-- certificates for JIT access. Before this migration only certificate
-- metadata read/revoke was granted, so certificate issuance via the API was
-- available to super administrators only.

INSERT INTO role_permissions (role_id, action) VALUES
    ('role-infrastructure-administrator', 'certificate.issue'),
    ('role-platform-engineer', 'certificate.issue'),
    ('role-linux-administrator', 'certificate.issue'),
    ('role-temporary-contractor', 'certificate.issue')
ON CONFLICT DO NOTHING;
