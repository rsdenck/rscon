DELETE FROM role_permissions WHERE action = 'certificate.issue'
    AND role_id IN (
        'role-infrastructure-administrator',
        'role-platform-engineer',
        'role-linux-administrator',
        'role-temporary-contractor'
    );
