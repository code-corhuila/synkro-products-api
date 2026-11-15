DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'products_reader') THEN
    CREATE ROLE products_reader NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE
      INHERIT NOREPLICATION NOBYPASSRLS PASSWORD NULL;
  ELSIF EXISTS (
    SELECT 1 FROM pg_authid
    WHERE rolname = 'products_reader'
      AND (rolcanlogin OR rolsuper OR rolcreatedb OR rolcreaterole
           OR rolreplication OR rolbypassrls OR rolpassword IS NOT NULL)
  ) OR EXISTS (
    SELECT 1 FROM pg_auth_members
    WHERE roleid = 'products_reader'::regrole OR member = 'products_reader'::regrole
  ) THEN
    RAISE EXCEPTION 'products_reader already exists with unsafe attributes or membership; inspect before granting access';
  END IF;

  -- A role with safe attributes can still carry old grants. Do not extend
  -- its access or silently revoke somebody else's grants; stop for review.
  IF EXISTS (
    SELECT 1 FROM pg_namespace n
    WHERE n.nspname NOT IN ('public', 'information_schema')
      AND n.nspname !~ '^pg_'
      AND (has_schema_privilege('products_reader', n.oid, 'CREATE')
        OR (n.nspname <> 'products_schema'
          AND has_schema_privilege('products_reader', n.oid, 'USAGE')))
  ) OR EXISTS (
    SELECT 1 FROM pg_class t JOIN pg_namespace n ON n.oid = t.relnamespace
    WHERE t.relkind IN ('r', 'p', 'v', 'm', 'f')
      AND n.nspname <> 'information_schema' AND n.nspname !~ '^pg_'
      AND (has_table_privilege('products_reader', t.oid,
            'INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER')
        OR ((n.nspname <> 'products_schema' OR t.relname = 'flyway_schema_history')
          AND has_table_privilege('products_reader', t.oid, 'SELECT')))
  ) OR EXISTS (
    SELECT 1 FROM pg_default_acl d CROSS JOIN LATERAL aclexplode(d.defaclacl) a
    WHERE a.grantee = 'products_reader'::regrole
      AND (d.defaclnamespace <> 'products_schema'::regnamespace
        OR d.defaclobjtype <> 'r' OR a.privilege_type <> 'SELECT' OR a.is_grantable)
  ) THEN
    RAISE EXCEPTION 'products_reader has unsafe existing permissions; inspect before granting access';
  END IF;
END $$;
