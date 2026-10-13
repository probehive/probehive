-- Organizations are identified by their generated id everywhere; the caller-chosen
-- slug had no remaining consumer (routes, API paths, and references all use the id,
-- and public status pages use their own token). Drop the column and its unique index.
DROP INDEX ux_organizations_slug;

ALTER TABLE organizations DROP COLUMN slug;
