-- The UNIQUE constraints on users.login and tags.name already index those
-- columns, and no query filters subscriptions by status.
DROP INDEX idx_users_login;
DROP INDEX idx_tags_name;
DROP INDEX idx_subscriptions_status;
